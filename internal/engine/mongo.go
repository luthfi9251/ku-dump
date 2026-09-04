package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"time"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type Mongo struct {
	tools ToolSet
	crypt *cryptx.Cryptx
}

func NewMongo(ts ToolSet, cx *cryptx.Cryptx) *Mongo {
	return &Mongo{tools: ts, crypt: cx}
}

func (m *Mongo) Name() string { return "mongodb" }

func (m *Mongo) RequiredTools() []string { return []string{"mongodump", "mongorestore"} }

func (m *Mongo) ToolsMissing() []string { return m.tools.Missing(m.RequiredTools()...) }

type mongoOptions struct {
	AuthSource string `json:"authSource"`
	ReplicaSet string `json:"replicaSet"`
}

func (m *Mongo) opts(db meta.Database) mongoOptions {
	var o mongoOptions
	_ = json.Unmarshal([]byte(db.Options), &o)
	if o.AuthSource == "" {
		o.AuthSource = "admin"
	}
	return o
}

func (m *Mongo) password(db meta.Database) string {
	pw, _ := m.crypt.Decrypt(db.PasswordEnc)
	return pw
}

func (m *Mongo) connArgs(db meta.Database) []string {
	args := []string{"--host", net.JoinHostPort(db.Host, strconv.Itoa(db.Port))}
	if db.Username != "" {
		args = append(args, "-u", db.Username, "-p", m.password(db),
			"--authenticationDatabase", m.opts(db).AuthSource)
	}
	if rs := m.opts(db).ReplicaSet; rs != "" {
		args = append(args, "--replicaSet", rs)
	}
	return args
}

func (m *Mongo) dumpArgs(db meta.Database) []string {
	args := []string{"--archive", "--gzip"}
	args = append(args, m.connArgs(db)...)
	return append(args, "--db", db.DBName)
}

func (m *Mongo) restoreArgs(db meta.Database, sourceDB string) []string {
	args := []string{"--archive", "--gzip", "--drop"}
	args = append(args, m.connArgs(db)...)
	if sourceDB != "" {
		args = append(args, "--nsInclude", sourceDB+".*")
		if sourceDB != db.DBName {
			args = append(args, "--nsFrom", sourceDB+".*", "--nsTo", db.DBName+".*")
		}
	}
	return args
}

func (m *Mongo) uri(db meta.Database) string {
	u := url.URL{Scheme: "mongodb", Host: net.JoinHostPort(db.Host, strconv.Itoa(db.Port))}
	if db.Username != "" {
		u.User = url.UserPassword(db.Username, m.password(db))
	}
	u.Path = "/" // driver requires a "/" before the query string
	q := url.Values{}
	q.Set("authSource", m.opts(db).AuthSource)
	q.Set("connectTimeoutMS", "5000")
	if rs := m.opts(db).ReplicaSet; rs != "" {
		q.Set("replicaSet", rs)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Mongo) TestConnection(ctx context.Context, db meta.Database) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(m.uri(db)))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background())
	return client.Ping(ctx, readpref.Primary())
}

func (m *Mongo) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	tool := m.tools["mongodump"]
	if tool == nil {
		return fmt.Errorf("%w: mongodump", ErrToolMissing)
	}
	args := append(append([]string{}, tool.Args[1:]...), m.dumpArgs(db)...)
	cmd := exec.CommandContext(ctx, tool.Args[0], args...)
	cmd.Stdout = out
	cmd.Stderr = log
	return cmd.Run()
}

func (m *Mongo) Restore(ctx context.Context, db meta.Database, sourceDB string, in io.Reader, log io.Writer) error {
	tool := m.tools["mongorestore"]
	if tool == nil {
		return fmt.Errorf("%w: mongorestore", ErrToolMissing)
	}
	args := append(append([]string{}, tool.Args[1:]...), m.restoreArgs(db, sourceDB)...)
	cmd := exec.CommandContext(ctx, tool.Args[0], args...)
	cmd.Stdin = in
	cmd.Stderr = log
	return cmd.Run()
}
