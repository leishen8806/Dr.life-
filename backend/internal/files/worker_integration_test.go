package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestVerificationWorkerRejectsTamperedRustFSUpload(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rustfs/rustfs:1.0.0-alpha.50",
			Env:          map[string]string{"RUSTFS_ACCESS_KEY": "testaccess", "RUSTFS_SECRET_KEY": "testsecret"},
			ExposedPorts: []string{"9000/tcp"},
			WaitingFor:   wait.ForListeningPort("9000/tcp").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		if os.Getenv("CI") == "" {
			t.Skipf("RustFS image unavailable: %v", err)
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
	if _, err := client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String("files")}); err != nil {
		t.Fatal(err)
	}

	objects := storage.NewMinIO(storage.MinIOConfig{Bucket: "files", Endpoint: endpoint, AccessKey: "testaccess", SecretKey: "testsecret"}, storage.Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100})
	verifier, ok := objects.(storage.ObjectVerifier)
	if !ok {
		t.Fatal("storage adapter does not expose worker verification access")
	}
	expected := sha256.Sum256([]byte("jpg"))
	put, err := objects.PresignPut(ctx, storage.PutRequest{ContentType: "image/jpeg", ContentLength: 3, ChecksumSHA256: base64.StdEncoding.EncodeToString(expected[:])})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, put.URL, bytes.NewReader([]byte("bad")))
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
		t.Fatalf("tampered upload status=%d, want 200 so the worker verifies it", response.StatusCode)
	}

	store := &verificationMemoryStore{file: File{PublicID: "file-1", ObjectKey: put.Key, ContentType: "image/jpeg", Size: 3, SHA256: expected[:], Status: StatusVerifying}}
	if err := NewVerificationWorker(store, verifier).Handle(ctx, asynq.NewTask(TaskVerify, []byte(`{"public_id":"file-1"}`))); err != nil {
		t.Fatal(err)
	}
	if store.file.Status != StatusRejected {
		t.Fatalf("status=%q, want rejected", store.file.Status)
	}
	contents, err := verifier.Open(ctx, put.Key)
	if err == nil {
		_, _ = io.Copy(io.Discard, contents)
		contents.Close()
		t.Fatal("tampered object remained after rejection")
	}
}

func TestVerificationWorkerMarksHonestRustFSUploadReady(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rustfs/rustfs:1.0.0-alpha.50",
			Env:          map[string]string{"RUSTFS_ACCESS_KEY": "testaccess", "RUSTFS_SECRET_KEY": "testsecret"},
			ExposedPorts: []string{"9000/tcp"},
			WaitingFor:   wait.ForListeningPort("9000/tcp").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		if os.Getenv("CI") == "" {
			t.Skipf("RustFS image unavailable: %v", err)
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
	if _, err := client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String("files")}); err != nil {
		t.Fatal(err)
	}

	objects := storage.NewMinIO(storage.MinIOConfig{Bucket: "files", Endpoint: endpoint, AccessKey: "testaccess", SecretKey: "testsecret"}, storage.Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100})
	verifier, ok := objects.(storage.ObjectVerifier)
	if !ok {
		t.Fatal("storage adapter does not expose worker verification access")
	}
	body := []byte("jpg")
	sum := sha256.Sum256(body)
	put, err := objects.PresignPut(ctx, storage.PutRequest{ContentType: "image/jpeg", ContentLength: int64(len(body)), ChecksumSHA256: base64.StdEncoding.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, put.URL, bytes.NewReader(body))
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
		t.Fatalf("honest upload status=%d, want 200", response.StatusCode)
	}

	store := &verificationMemoryStore{file: File{PublicID: "file-1", ObjectKey: put.Key, ContentType: "image/jpeg", Size: int64(len(body)), SHA256: sum[:], Status: StatusVerifying}}
	if err := NewVerificationWorker(store, verifier).Handle(ctx, asynq.NewTask(TaskVerify, []byte(`{"public_id":"file-1"}`))); err != nil {
		t.Fatal(err)
	}
	if store.file.Status != StatusReady {
		t.Fatalf("status=%q, want ready", store.file.Status)
	}
	if store.file.ObjectKey == put.Key {
		t.Fatal("ready file retained the presigned PUT key")
	}
	contents, err := verifier.Open(ctx, store.file.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(contents)
	contents.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("promoted object = %q, want %q", got, body)
	}
}
