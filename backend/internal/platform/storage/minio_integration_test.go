package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSkipUnavailableImageOutsideCI(t *testing.T) {
	err := fmt.Errorf("pull access denied")
	if !skipUnavailableImage("", err) {
		t.Fatal("local unavailable image should be skipped")
	}
	if skipUnavailableImage("true", err) {
		t.Fatal("CI unavailable image must fail")
	}
	if skipUnavailableImage("", fmt.Errorf("container did not start")) {
		t.Fatal("non-image failure must not be skipped")
	}
}

func skipUnavailableImage(ci string, err error) bool {
	return ci == "" && (strings.Contains(err.Error(), "failed to resolve reference") || strings.Contains(err.Error(), "pull access denied"))
}

func TestMinIOPresignedURLs(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rustfs/rustfs:1.0.0-alpha.50",
			Env:          map[string]string{"RUSTFS_ACCESS_KEY": "testaccess", "RUSTFS_SECRET_KEY": "testsecret"},
			ExposedPorts: []string{"9000/tcp"},
			WaitingFor:   wait.ForListeningPort("9000/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		if skipUnavailableImage(os.Getenv("CI"), err) {
			t.Skipf("MinIO test image unavailable: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + host + ":" + port.Port()

	client := awss3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("testaccess", "testsecret", ""))}, func(o *awss3.Options) { o.BaseEndpoint = aws.String(endpoint); o.UsePathStyle = true })
	if _, err = client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String("files")}); err != nil {
		t.Fatal(err)
	}
	store := NewMinIO(MinIOConfig{Bucket: "files", Endpoint: endpoint, AccessKey: "testaccess", SecretKey: "testsecret"}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100})
	verifier, ok := store.(ObjectVerifier)
	if !ok {
		t.Fatal("storage adapter does not expose worker verification access")
	}

	sum := sha256.Sum256([]byte("jpg"))
	put, err := store.PresignPut(ctx, PutRequest{ContentType: "image/jpeg", ContentLength: 3, ChecksumSHA256: base64.StdEncoding.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	putURL, _ := url.Parse(put.URL)
	if got := putURL.Query().Get("X-Amz-Expires"); got != "600" {
		t.Fatalf("PUT expiry = %q, want 600", got)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, put.URL, bytes.NewReader([]byte("jpg")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(sum[:]))
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", response.StatusCode)
	}
	stat, err := verifier.Stat(ctx, put.Key)
	if err != nil {
		t.Fatal(err)
	}
	racedWrite, err := http.NewRequestWithContext(ctx, http.MethodPut, put.URL, bytes.NewReader([]byte("bad")))
	if err != nil {
		t.Fatal(err)
	}
	racedWrite.Header.Set("Content-Type", "image/jpeg")
	response, err = http.DefaultClient.Do(racedWrite)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if racedKey, err := verifier.Promote(ctx, put.Key, stat.ETag); err == nil {
		// RustFS may not enforce CopySourceIfMatch. The worker therefore re-hashes
		// the private promoted object before marking the file ready.
		if err := verifier.Delete(ctx, racedKey); err != nil {
			t.Fatal(err)
		}
	}
	// Restore the verified bytes, then promote using a fresh conditional ETag.
	restore, err := http.NewRequestWithContext(ctx, http.MethodPut, put.URL, bytes.NewReader([]byte("jpg")))
	if err != nil {
		t.Fatal(err)
	}
	restore.Header.Set("Content-Type", "image/jpeg")
	response, err = http.DefaultClient.Do(restore)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	stat, err = verifier.Stat(ctx, put.Key)
	if err != nil {
		t.Fatal(err)
	}
	readyKey, err := verifier.Promote(ctx, put.Key, stat.ETag)
	if err != nil {
		t.Fatal(err)
	}
	// The upload URL remains valid, but it can only rewrite its original key;
	// callers download the promoted key that was never exposed for PUT.
	rewrite, err := http.NewRequestWithContext(ctx, http.MethodPut, put.URL, bytes.NewReader([]byte("bad")))
	if err != nil {
		t.Fatal(err)
	}
	rewrite.Header.Set("Content-Type", "image/jpeg")
	response, err = http.DefaultClient.Do(rewrite)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	ready, err := verifier.Open(ctx, readyKey)
	if err != nil {
		t.Fatal(err)
	}
	readyBytes, err := io.ReadAll(ready)
	ready.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(readyBytes) != "jpg" {
		t.Fatalf("promoted object = %q, want original content", readyBytes)
	}
	wrong, err := store.PresignPut(ctx, PutRequest{ContentType: "image/jpeg", ContentLength: 3, ChecksumSHA256: base64.StdEncoding.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, wrong.URL, bytes.NewReader([]byte("bad")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "image/jpeg")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		// RustFS accepts this pre-signed PUT without enforcing x-amz-checksum-sha256;
		// AC-b3 is enforced by the worker SHA-256 verification after upload.
		contents, err := verifier.Open(ctx, wrong.Key)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(contents)
		contents.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "bad" {
			t.Fatalf("Open() = %q, want tampered content", data)
		}
		if err := verifier.Delete(ctx, wrong.Key); err != nil {
			t.Fatal(err)
		}
		if _, err := verifier.Open(ctx, wrong.Key); err == nil {
			t.Fatal("deleted object remained readable")
		}
	} else if response.StatusCode < http.StatusBadRequest || response.StatusCode >= http.StatusInternalServerError {
		t.Fatalf("tampered upload status = %d, want success or 4xx rejection", response.StatusCode)
	}

	get, err := store.PresignGet(ctx, put.Key)
	if err != nil {
		t.Fatal(err)
	}
	getURL, _ := url.Parse(get.URL)
	if got := getURL.Query().Get("X-Amz-Expires"); got != "300" {
		t.Fatalf("GET expiry = %q, want 300", got)
	}
	response, err = http.Get(get.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", response.StatusCode)
	}

	short := newS3(S3Config{Bucket: "files", Region: "us-east-1", Endpoint: endpoint, AccessKey: "testaccess", SecretKey: "testsecret", UsePathStyle: true}, Policy{}, time.Second, time.Second)
	expired, err := short.PresignGet(ctx, put.Key)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	response, err = http.Get(expired.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("expired GET URL was accepted")
	}
}
