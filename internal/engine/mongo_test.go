package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-dump/internal/meta"
)

func mongoDB(t *testing.T, user string) meta.Database {
	t.Helper()
	cx := mustCrypt(t)
	enc, err := cx.Encrypt("pass")
	if err != nil {
		t.Fatal(err)
	}
	return meta.Database{ID: 2, Name: "mongo-prod", Engine: "mongodb", Host: "m1", Port: 27017,
		DBName: "shop", Username: user, PasswordEnc: enc, Options: `{"authSource":"users"}`}
}

func newMongo(t *testing.T) *Mongo {
	dir := t.TempDir()
	dump := writeScript(t, dir, "mongodump.sh", "#!/bin/sh\necho \"$@\" >&2\nprintf 'gzipfake'\nexit 0\n")
	restore := writeScript(t, dir, "mongorestore.sh", "#!/bin/sh\ncat >/dev/null\necho \"$@\" >&2\nexit 0\n")
	ts := ToolSet{
		"mongodump":    {Name: "mongodump", Args: []string{dump}},
		"mongorestore": {Name: "mongorestore", Args: []string{restore}},
	}
	return NewMongo(ts, mustCrypt(t))
}

func TestMongoDumpArgsWithAuth(t *testing.T) {
	m := newMongo(t)
	db := mongoDB(t, "alice")
	joined := strings.Join(m.dumpArgs(db), " ")
	for _, want := range []string{"--archive", "--gzip", "--host m1:27017", "-u alice", "-p pass", "--authenticationDatabase users", "--db shop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("dumpArgs missing %q: %s", want, joined)
		}
	}
}

func TestMongoDumpArgsNoAuth(t *testing.T) {
	m := newMongo(t)
	db := mongoDB(t, "")
	joined := strings.Join(m.dumpArgs(db), " ")
	if strings.Contains(joined, "-u ") || strings.Contains(joined, "-p ") || strings.Contains(joined, "--authenticationDatabase") {
		t.Fatalf("auth flags leaked: %s", joined)
	}
}

func TestMongoRestoreArgsSameDB(t *testing.T) {
	m := newMongo(t)
	db := mongoDB(t, "alice")
	joined := strings.Join(m.restoreArgs(db, "shop"), " ")
	for _, want := range []string{"--archive", "--gzip", "--drop", "--nsInclude shop.*"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "--nsFrom") {
		t.Fatalf("nsFrom should be absent for same db: %s", joined)
	}
}

func TestMongoRestoreArgsRemap(t *testing.T) {
	m := newMongo(t)
	db := mongoDB(t, "alice")
	joined := strings.Join(m.restoreArgs(db, "shop_prod"), " ")
	for _, want := range []string{"--nsInclude shop_prod.*", "--nsFrom shop_prod.*", "--nsTo shop.*"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q: %s", want, joined)
		}
	}
}

func TestMongoRestoreArgsNoSource(t *testing.T) {
	m := newMongo(t)
	db := mongoDB(t, "alice")
	joined := strings.Join(m.restoreArgs(db, ""), " ")
	if strings.Contains(joined, "--nsInclude") || strings.Contains(joined, "--nsFrom") {
		t.Fatalf("ns flags leaked for upload: %s", joined)
	}
}

func TestMongoDumpRestoreRun(t *testing.T) {
	m := newMongo(t)
	db := mongoDB(t, "alice")
	var out, log bytes.Buffer
	if err := m.Dump(context.Background(), db, &out, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "gzipfake") {
		t.Fatalf("out = %q", out.String())
	}
	if err := m.Restore(context.Background(), db, "shop", strings.NewReader(out.String()), &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "--drop") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestMongoToolsMissing(t *testing.T) {
	m := NewMongo(ToolSet{}, mustCrypt(t))
	if got := m.ToolsMissing(); len(got) != 2 {
		t.Fatalf("ToolsMissing = %v", got)
	}
}
