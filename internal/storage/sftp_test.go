package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewSFTPValidation(t *testing.T) {
	ok := SFTPConfig{Host: "h", Username: "u", AuthType: "password", Secret: "s"}
	if _, err := NewSFTP(ok); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := []SFTPConfig{
		{},
		{Username: "u", AuthType: "password", Secret: "s"},
		{Host: "h", AuthType: "password", Secret: "s"},
		{Host: "h", Username: "u", Secret: "s"},
		{Host: "h", Username: "u", AuthType: "password"},
		{Host: "h", Username: "u", AuthType: "token", Secret: "s"},
	}
	for i, cfg := range bad {
		if _, err := NewSFTP(cfg); !errors.Is(err, ErrSFTPNotConfigured) {
			t.Fatalf("case %d: err = %v, want ErrSFTPNotConfigured", i, err)
		}
	}
	s, _ := NewSFTP(ok)
	if s.Kind() != "sftp" {
		t.Fatalf("kind = %q", s.Kind())
	}
}

func TestSFTPRemotePathRejectsTraversal(t *testing.T) {
	s, _ := NewSFTP(SFTPConfig{Host: "h", Username: "u", AuthType: "password", Secret: "s", RemoteDir: "/srv"})
	if _, err := s.remotePath("../escape.dump"); err == nil {
		t.Fatal("traversal key accepted")
	}
	p, err := s.remotePath("postgres/db/x.dump")
	if err != nil || p != "/srv/postgres/db/x.dump" {
		t.Fatalf("path = %q, %v", p, err)
	}
}

func TestSFTPOpsFailWithoutServer(t *testing.T) {
	// port 1 on localhost is closed; every op must fail cleanly, not hang.
	s, _ := NewSFTP(SFTPConfig{Host: "127.0.0.1", Port: 1, Username: "u",
		AuthType: "password", Secret: "s"})
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- s.Test(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Test succeeded without a server")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Test hung without a server")
	}
	if _, err := s.Open(ctx, "k"); err == nil {
		t.Fatal("Open succeeded without a server")
	}
	src := putFile(t, "x")
	if err := s.Put(ctx, "k", src); err == nil {
		t.Fatal("Put succeeded without a server")
	}
	if err := s.Delete(ctx, "k"); err == nil {
		t.Fatal("Delete succeeded without a server")
	}
	if err := s.Stat(ctx, "../x"); err == nil {
		t.Fatal("Stat accepted traversal key")
	}
}
