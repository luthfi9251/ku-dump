package storage

import (
	"errors"
	"testing"
)

func TestNewS3Validation(t *testing.T) {
	full := S3Config{Endpoint: "127.0.0.1:9000", Bucket: "b", AccessKey: "a", SecretKey: "s"}
	if _, err := NewS3(full); err != nil {
		t.Fatalf("full config rejected: %v", err)
	}
	bad := []S3Config{
		{},
		{Bucket: "b", AccessKey: "a", SecretKey: "s"},
		{Endpoint: "e", AccessKey: "a", SecretKey: "s"},
		{Endpoint: "e", Bucket: "b", SecretKey: "s"},
		{Endpoint: "e", Bucket: "b", AccessKey: "a"},
	}
	for i, cfg := range bad {
		if _, err := NewS3(cfg); !errors.Is(err, ErrS3NotConfigured) {
			t.Fatalf("case %d: err = %v, want ErrS3NotConfigured", i, err)
		}
	}
}

func TestS3SchemeStripping(t *testing.T) {
	s, err := NewS3(S3Config{Endpoint: "http://127.0.0.1:9000", Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if s.endpoint != "127.0.0.1:9000" {
		t.Fatalf("endpoint = %q", s.endpoint)
	}
	if s.secure {
		t.Fatal("http endpoint should be insecure")
	}
	s2, _ := NewS3(S3Config{Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if s2.endpoint != "s3.example.com" || !s2.secure {
		t.Fatalf("endpoint = %q secure = %v", s2.endpoint, s2.secure)
	}
}

func TestS3ObjectKey(t *testing.T) {
	s, _ := NewS3(S3Config{Endpoint: "e", Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if got := s.object("x/y.dump"); got != "x/y.dump" {
		t.Fatalf("object = %q", got)
	}
	s.cfg.Prefix = "ku-dump"
	if got := s.object("x/y.dump"); got != "ku-dump/x/y.dump" {
		t.Fatalf("object = %q", got)
	}
	if s.Kind() != "s3" {
		t.Fatalf("kind = %q", s.Kind())
	}
}
