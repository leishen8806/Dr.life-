package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestCleanupPostgresAndRustFS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dbContainer, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("files_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(dbContainer) })
	dsn, err := dbContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(sqlDB, filesMigrationDir(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	objectContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "rustfs/rustfs:1.0.0-alpha.50", Env: map[string]string{"RUSTFS_ACCESS_KEY": "testaccess", "RUSTFS_SECRET_KEY": "testsecret"}, ExposedPorts: []string{"9000/tcp"}, WaitingFor: wait.ForListeningPort("9000/tcp")}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = objectContainer.Terminate(ctx) })
	host, err := objectContainer.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := objectContainer.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + host + ":" + port.Port()
	client := awss3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("testaccess", "testsecret", ""))}, func(o *awss3.Options) { o.BaseEndpoint = aws.String(endpoint); o.UsePathStyle = true })
	if _, err := client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String("files")}); err != nil {
		t.Fatal(err)
	}
	objects := storage.NewMinIO(storage.MinIOConfig{Bucket: "files", Endpoint: endpoint, AccessKey: "testaccess", SecretKey: "testsecret"}, storage.Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100})
	verifier := objects.(storage.ObjectVerifier)
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	rows := []struct {
		id, status         string
		created, retention time.Time
	}{
		{"pending-old", "pending", now.Add(-25 * time.Hour), now.Add(time.Hour)},
		{"ready-expired", "ready", now.Add(-time.Hour), now.Add(-time.Second)},
		{"verifying-old", "verifying", now.Add(-25 * time.Hour), now.Add(-time.Hour)},
	}
	for i, row := range rows {
		key := fmt.Sprintf("objects/%032x", i+1)
		if _, err := client.PutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String("files"), Key: aws.String(key), Body: nil}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO ops.file(public_id,owner_account,purpose,object_key,content_type,size,sha256,status,retention_until,created_at,updated_at) VALUES($1,1,'product_image',$2,'image/jpeg',1,decode(repeat('00',32),'hex'),$3,$4,$5,$5)`, row.id, key, row.status, row.retention, row.created); err != nil {
			t.Fatal(err)
		}
	}
	result, err := (PostgresStore{DB: pool}).Cleanup(ctx, now, verifier.Delete)
	if err != nil {
		t.Fatal(err)
	}
	if result.Expired != 1 || result.Deleted != 1 {
		t.Fatalf("cleanup=%+v", result)
	}
	for _, want := range []struct{ id, status string }{{"pending-old", "expired"}, {"ready-expired", "deleted"}, {"verifying-old", "verifying"}} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT status FROM ops.file WHERE public_id=$1`, want.id).Scan(&got); err != nil || got != want.status {
			t.Fatalf("%s status=%q err=%v", want.id, got, err)
		}
	}
	second, err := (PostgresStore{DB: pool}).Cleanup(ctx, now, verifier.Delete)
	if err != nil || second != (CleanupResult{}) {
		t.Fatalf("second=%+v err=%v", second, err)
	}

	// Two workers racing the same ready object must claim it only once. The
	// PostgreSQL row lock makes the second worker observe no candidate rather
	// than deleting an already-promoted object or overwriting the terminal state.
	raceKey := "objects/00000000000000000000000000000099"
	if _, err := client.PutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String("files"), Key: aws.String(raceKey), Body: nil}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ops.file(public_id,owner_account,purpose,object_key,content_type,size,sha256,status,retention_until,created_at,updated_at) VALUES('ready-race',1,'product_image',$1,'image/jpeg',1,decode(repeat('00',32),'hex'),'ready',$2,$3,$3)`, raceKey, now.Add(-time.Second), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	results := make(chan CleanupResult, 2)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			result, cleanupErr := (PostgresStore{DB: pool}).Cleanup(ctx, now, verifier.Delete)
			results <- result
			errs <- cleanupErr
		}()
	}
	group.Wait()
	close(results)
	close(errs)
	var deleted int
	for cleanupErr := range errs {
		if cleanupErr != nil {
			t.Fatal(cleanupErr)
		}
	}
	for result := range results {
		deleted += result.Deleted
	}
	if deleted != 1 {
		t.Fatalf("concurrent cleanup deleted=%d, want 1", deleted)
	}
	var raceStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM ops.file WHERE public_id='ready-race'`).Scan(&raceStatus); err != nil || raceStatus != "deleted" {
		t.Fatalf("ready-race status=%q err=%v", raceStatus, err)
	}

	// MarkReady persists the bytes that are actually retained after image
	// sanitation, rather than the pre-sanitization upload declaration.
	publishedKey := "objects/00000000000000000000000000000098"
	published := []byte("sanitized-published-object")
	publishedSHA256 := sha256.Sum256(published)
	if _, err := client.PutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String("files"), Key: aws.String(publishedKey), Body: bytes.NewReader(published)}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ops.file(public_id,owner_account,purpose,object_key,content_type,size,sha256,status,retention_until,created_at,updated_at) VALUES('published-metadata',1,'product_image',$1,'image/jpeg',1,decode(repeat('00',32),'hex'),'verifying',$2,$3,$3)`, publishedKey, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if err := (PostgresStore{DB: pool}).MarkReady(ctx, "published-metadata", publishedKey, int64(len(published)), publishedSHA256[:]); err != nil {
		t.Fatal(err)
	}
	var persistedSize int64
	var persistedSHA256 []byte
	var persistedStatus string
	if err := pool.QueryRow(ctx, `SELECT size, sha256, status FROM ops.file WHERE public_id='published-metadata'`).Scan(&persistedSize, &persistedSHA256, &persistedStatus); err != nil {
		t.Fatal(err)
	}
	body, err := verifier.Open(ctx, publishedKey)
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read published object: read=%v close=%v", readErr, closeErr)
	}
	if persistedStatus != "ready" || persistedSize != int64(len(actual)) || !bytes.Equal(persistedSHA256, sha256Sum(actual)) {
		t.Fatalf("published metadata status=%q size=%d sha256=%x object size=%d sha256=%x", persistedStatus, persistedSize, persistedSHA256, len(actual), sha256Sum(actual))
	}
	t.Logf("cleanup database result expired=%d deleted=%d; second run expired=%d deleted=%d", result.Expired, result.Deleted, second.Expired, second.Deleted)
}

func sha256Sum(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}

func filesMigrationDir(t *testing.T) string {
	t.Helper()
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("migration path")
	}
	return filepath.Join(filepath.Dir(f), "..", "..", "migrations")
}
