package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/meta"
)

var ErrToolMissing = errors.New("required tool not found")

type Postgres struct {
	tools ToolSet
	crypt *cryptx.Cryptx
}

func NewPostgres(ts ToolSet, cx *cryptx.Cryptx) *Postgres {
	return &Postgres{tools: ts, crypt: cx}
}

func (p *Postgres) Name() string { return "postgres" }

func (p *Postgres) RequiredTools() []string { return []string{"pg_dump", "pg_restore"} }

func (p *Postgres) ToolsMissing() []string { return p.tools.Missing(p.RequiredTools()...) }

type pgOptions struct {
	SSLMode string `json:"sslmode"`
}

func (p *Postgres) sslMode(db meta.Database) string {
	var o pgOptions
	_ = json.Unmarshal([]byte(db.Options), &o)
	if o.SSLMode == "" {
		return "prefer"
	}
	return o.SSLMode
}

func (p *Postgres) password(db meta.Database) string {
	pw, _ := p.crypt.Decrypt(db.PasswordEnc)
	return pw
}

func (p *Postgres) connArgs(db meta.Database) []string {
	return []string{"-h", db.Host, "-p", strconv.Itoa(db.Port), "-U", db.Username, "-d", db.DBName}
}

func (p *Postgres) dumpArgs(db meta.Database) []string {
	// -v: verbose progress on stderr; without it a successful dump is silent
	// and the job log stays empty.
	args := []string{"-Fc", "-v", "--no-owner", "--no-privileges"}
	return append(args, p.connArgs(db)...)
}

func (p *Postgres) restoreArgs(db meta.Database) []string {
	args := []string{"-v", "--clean", "--if-exists", "--no-owner", "--no-privileges"}
	return append(args, p.connArgs(db)...)
}

func (p *Postgres) uri(db meta.Database) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		url.QueryEscape(db.Username), url.QueryEscape(p.password(db)),
		db.Host, db.Port, db.DBName, p.sslMode(db))
}

func (p *Postgres) TestConnection(ctx context.Context, db meta.Database) error {
	conn, err := pgx.Connect(ctx, p.uri(db))
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return conn.Ping(ctx)
}

type stderrCapture struct {
	w   io.Writer
	buf []byte
}

func (c *stderrCapture) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	return c.w.Write(p)
}

func runTool(cmd *exec.Cmd, stderr io.Writer) error {
	sc := &stderrCapture{w: stderr}
	cmd.Stderr = sc
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%w: %s", err, sc.buf)
	}
	return nil
}

func (p *Postgres) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	tool := p.tools["pg_dump"]
	if tool == nil {
		return fmt.Errorf("%w: pg_dump", ErrToolMissing)
	}
	args := append(append([]string{}, tool.Args[1:]...), p.dumpArgs(db)...)
	cmd := exec.CommandContext(ctx, tool.Args[0], args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+p.password(db))
	cmd.Stdout = out
	return runTool(cmd, log)
}

func (p *Postgres) Restore(ctx context.Context, db meta.Database, sourceDB string, in io.Reader, log io.Writer) error {
	tool := p.tools["pg_restore"]
	if tool == nil {
		return fmt.Errorf("%w: pg_restore", ErrToolMissing)
	}
	args := append(append([]string{}, tool.Args[1:]...), p.restoreArgs(db)...)
	cmd := exec.CommandContext(ctx, tool.Args[0], args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+p.password(db))
	cmd.Stdin = in
	return runTool(cmd, log)
}
