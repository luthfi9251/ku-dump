package engine

import (
	"context"
	"io"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/meta"
)

type Postgres struct{}

func NewPostgres(ts ToolSet, cx *cryptx.Cryptx) *Postgres { return &Postgres{} }

func (p *Postgres) Name() string { return "postgres" }

func (p *Postgres) RequiredTools() []string { return nil }

func (p *Postgres) ToolsMissing() []string { return nil }

func (p *Postgres) TestConnection(ctx context.Context, db meta.Database) error { return nil }

func (p *Postgres) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	return nil
}

func (p *Postgres) Restore(ctx context.Context, db meta.Database, sourceDB string, in io.Reader, log io.Writer) error {
	return nil
}
