package storage

import (
	"bytes"
	"context"
	"fmt"
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

	put, err := store.PresignPut(ctx, PutRequest{ContentType: "image/jpeg", ContentLength: 3})
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
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", response.StatusCode)
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
