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
	nameRe = regexp.MustCompile(`[^a-z0-9._/-]+`)
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

// BuildKey resolves a storage key from an optional folder path and filename
// pattern. Empty path+pattern falls back to KeyFor. Pattern variables:
// {workflow} {db} {engine} {date} {time} {timestamp}. Any pattern without
// {timestamp} gets -<timestamp> inserted before .dump so keys are unique and
// existing dumps are never overwritten.
func BuildKey(path, pattern, engineName, db, workflow string, t time.Time) string {
	if path == "" && pattern == "" {
		return KeyFor(engineName, Slug(db), t)
	}
	if pattern == "" {
		pattern = Slug(db)
	}
	pattern = strings.ToLower(pattern)
	stamp := t.UTC().Format("20060102-150405")
	if !strings.Contains(pattern, "{timestamp}") {
		pattern += "-{timestamp}"
	}
	repl := strings.NewReplacer(
		"{workflow}", Slug(workflow),
		"{db}", Slug(db),
		"{engine}", Slug(engineName),
		"{date}", t.UTC().Format("2006-01-02"),
		"{time}", t.UTC().Format("15-04-05"),
		"{timestamp}", stamp,
	)
	name := repl.Replace(pattern)
	name = nameRe.ReplaceAllString(name, "-")
	name = dashRe.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-./")
	if name == "" {
		name = stamp
	}
	if !strings.HasSuffix(name, ".dump") {
		name += ".dump"
	}
	var segs []string
	for _, seg := range strings.Split(path, "/") {
		if seg = strings.TrimSpace(seg); seg == "" || seg == "." || seg == ".." {
			continue
		}
		segs = append(segs, Slug(seg))
	}
	key := strings.Join(segs, "/")
	if key != "" {
		key += "/"
	}
	return key + name
}
