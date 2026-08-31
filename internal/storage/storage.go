package storage

import (
	"context"
	"io"
	"regexp"
	"strings"
	"time"
)

type Store interface {
	Kind() string
	Put(ctx context.Context, key, localPath string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

var (
	slugRe = regexp.MustCompile(`[^a-z0-9-]+`)
	dashRe = regexp.MustCompile(`-{2,}`)
)

func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = dashRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "db"
	}
	return s
}

func KeyFor(engine, slug string, t time.Time) string {
	return engine + "/" + slug + "/" + t.UTC().Format("20060102-150405") + ".dump"
}
