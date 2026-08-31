package storage

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var ErrS3NotConfigured = errors.New("s3 storage not configured")

type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
}

type S3 struct {
	client   *minio.Client
	endpoint string
	secure   bool
	cfg      S3Config
}

func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, ErrS3NotConfigured
	}
	endpoint := cfg.Endpoint
	secure := true
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		endpoint = strings.TrimPrefix(endpoint, "http://")
		secure = false
	case strings.HasPrefix(endpoint, "https://"):
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	return &S3{client: client, endpoint: endpoint, secure: secure, cfg: cfg}, nil
}

func (s *S3) Kind() string { return "s3" }

func (s *S3) object(key string) string {
	if s.cfg.Prefix != "" {
		return s.cfg.Prefix + "/" + key
	}
	return key
}

func (s *S3) Put(ctx context.Context, key, localPath string) error {
	_, err := s.client.FPutObject(ctx, s.cfg.Bucket, s.object(key), localPath, minio.PutObjectOptions{})
	return err
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.cfg.Bucket, s.object(key), minio.GetObjectOptions{})
}

func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.cfg.Bucket, s.object(key), minio.RemoveObjectOptions{})
}

func (s *S3) Test(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.cfg.Bucket)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("bucket does not exist: " + s.cfg.Bucket)
	}
	return nil
}

func (s *S3) RawClient() (*minio.Client, error) {
	return s.client, nil
}
