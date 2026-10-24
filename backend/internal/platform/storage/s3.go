package storage

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
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
	return &s3Storage{bucket: config.Bucket, client: awss3.NewPresignClient(client), policy: policy, putExpires: putExpires, getExpires: getExpires}
}

func (s *s3Storage) PresignPut(ctx context.Context, in PutRequest) (SignedURL, error) {
	if err := s.policy.validate(in); err != nil {
		return SignedURL{}, err
	}
	key := randomKey()
	request, err := s.client.PresignPutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(in.ContentType), ContentLength: aws.Int64(in.ContentLength)}, func(o *awss3.PresignOptions) { o.Expires = s.putExpires })
	if err != nil {
		return SignedURL{}, err
	}
	return SignedURL{Key: key, URL: request.URL, ExpiresAt: time.Now().Add(s.putExpires)}, nil
}

func (s *s3Storage) PresignGet(ctx context.Context, key string) (SignedURL, error) {
	request, err := s.client.PresignGetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, func(o *awss3.PresignOptions) { o.Expires = s.getExpires })
	if err != nil {
		return SignedURL{}, err
	}
	return SignedURL{Key: key, URL: request.URL, ExpiresAt: time.Now().Add(s.getExpires)}, nil
}
