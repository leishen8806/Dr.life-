package storage

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Config struct {
	Bucket       string
	Region       string
	Endpoint     string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

type s3Storage struct {
	bucket     string
	objects    *awss3.Client
	client     *awss3.PresignClient
	policy     Policy
	putExpires time.Duration
	getExpires time.Duration
}

func NewS3(config S3Config, policy Policy) Storage {
	return newS3(config, policy, putTTL, getTTL)
}

func newS3(config S3Config, policy Policy, putExpires, getExpires time.Duration) Storage {
	region := config.Region
	if region == "" {
		region = "us-east-1"
	}
	client := awss3.NewFromConfig(aws.Config{Region: region, Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, ""))}, func(o *awss3.Options) {
		if config.Endpoint != "" {
			o.BaseEndpoint = aws.String(config.Endpoint)
		}
		o.UsePathStyle = config.UsePathStyle
	})
	return &s3Storage{bucket: config.Bucket, objects: client, client: awss3.NewPresignClient(client), policy: policy, putExpires: putExpires, getExpires: getExpires}
}

func (s *s3Storage) PresignPut(ctx context.Context, in PutRequest) (SignedURL, error) {
	if err := s.policy.validate(in); err != nil {
		return SignedURL{}, err
	}
	key := randomKey()
	request, err := s.client.PresignPutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(in.ContentType), ContentLength: aws.Int64(in.ContentLength), ChecksumAlgorithm: types.ChecksumAlgorithmSha256, ChecksumSHA256: aws.String(in.ChecksumSHA256)}, func(o *awss3.PresignOptions) { o.Expires = s.putExpires })
	if err != nil {
		return SignedURL{}, err
	}
	headers := make(map[string]string, len(request.SignedHeader))
	for name, values := range request.SignedHeader {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}
	if headers["X-Amz-Checksum-Sha256"] == "" {
		headers["X-Amz-Checksum-Sha256"] = in.ChecksumSHA256
	}
	return SignedURL{Key: key, URL: request.URL, ExpiresAt: time.Now().Add(s.putExpires), Headers: headers}, nil
}

func (s *s3Storage) Stat(ctx context.Context, key string) (ObjectStat, error) {
	if !validRandomKey(key) {
		return ObjectStat{}, ErrInvalidObjectKey
	}
	object, err := s.objects.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return ObjectStat{}, err
	}
	return ObjectStat{Size: aws.ToInt64(object.ContentLength), ContentType: aws.ToString(object.ContentType), ChecksumSHA256: aws.ToString(object.ChecksumSHA256)}, nil
}

func (s *s3Storage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validRandomKey(key) {
		return nil, ErrInvalidObjectKey
	}
	object, err := s.objects.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return object.Body, nil
}

func (s *s3Storage) Delete(ctx context.Context, key string) error {
	if !validRandomKey(key) {
		return ErrInvalidObjectKey
	}
	_, err := s.objects.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *s3Storage) PresignGet(ctx context.Context, key string) (SignedURL, error) {
	if !validRandomKey(key) {
		return SignedURL{}, ErrInvalidObjectKey
	}
	request, err := s.client.PresignGetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, func(o *awss3.PresignOptions) { o.Expires = s.getExpires })
	if err != nil {
		return SignedURL{}, err
	}
	return SignedURL{Key: key, URL: request.URL, ExpiresAt: time.Now().Add(s.getExpires)}, nil
}
