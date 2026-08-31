package engine

import (
	"context"
	"io"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/meta"
)

type Mongo struct{}

func NewMongo(ts ToolSet, cx *cryptx.Cryptx) *Mongo { return &Mongo{} }

func (m *Mongo) Name() string { return "mongodb" }

func (m *Mongo) RequiredTools() []string { return nil }

func (m *Mongo) ToolsMissing() []string { return nil }

func (m *Mongo) TestConnection(ctx context.Context, db meta.Database) error { return nil }

func (m *Mongo) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	return nil
}

func (m *Mongo) Restore(ctx context.Context, db meta.Database, sourceDB string, in io.Reader, log io.Writer) error {
	return nil
}
