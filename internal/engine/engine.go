package engine

import (
	"context"
	"io"
	"os/exec"
	"strings"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/meta"
)

type Engine interface {
	Name() string
	RequiredTools() []string
	ToolsMissing() []string
	TestConnection(ctx context.Context, db meta.Database) error
	Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error
	Restore(ctx context.Context, db meta.Database, sourceDB string, in io.Reader, log io.Writer) error
}

type Tool struct {
	Name string
	Args []string
}

type ToolSet map[string]*Tool

var ToolNames = []string{"pg_dump", "pg_restore", "mongodump", "mongorestore"}

func ResolveTools(overrides map[string]string) ToolSet {
	ts := ToolSet{}
	for _, name := range ToolNames {
		if ov := overrides[name]; ov != "" {
			fields := strings.Fields(ov)
			if len(fields) == 0 {
				continue
			}
			if path, err := exec.LookPath(fields[0]); err == nil {
				ts[name] = &Tool{Name: name, Args: append([]string{path}, fields[1:]...)}
			}
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			ts[name] = &Tool{Name: name, Args: []string{path}}
		}
	}
	return ts
}

func (ts ToolSet) Missing(names ...string) []string {
	var out []string
	for _, n := range names {
		if ts[n] == nil {
			out = append(out, n)
		}
	}
	return out
}

func BuildEngines(overrides map[string]string, cx *cryptx.Cryptx) map[string]Engine {
	ts := ResolveTools(overrides)
	return map[string]Engine{
		"postgres": NewPostgres(ts, cx),
		"mongodb":  NewMongo(ts, cx),
	}
}
