package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type LocalFS struct {
	root string
}

func NewLocalFS(root string) (*LocalFS, error) {
	if root == "" {
		return nil, errors.New("local storage root is empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalFS{root: root}, nil
}

func (l *LocalFS) Kind() string { return "local" }

// Test verifies the root exists (created if needed) and is writable by
// writing, reading and removing a probe file.
func (l *LocalFS) Test() error {
	if err := os.MkdirAll(l.root, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(l.root, "._kudump-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	if _, err := os.ReadFile(probe); err != nil {
		return err
	}
	return os.Remove(probe)
}

func (l *LocalFS) path(key string) (string, error) {
	p := filepath.Join(l.root, filepath.FromSlash(key))
	if !strings.HasPrefix(p, filepath.Clean(l.root)+string(os.PathSeparator)) {
		return "", errors.New("invalid storage key")
	}
	return p, nil
}

func (l *LocalFS) Put(ctx context.Context, key, localPath string) error {
	dst, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()
	// O_EXCL: a dump file may never be overwritten once written.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (l *LocalFS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (l *LocalFS) Delete(ctx context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
