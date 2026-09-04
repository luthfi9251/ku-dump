package meta

import (
	"context"
	"time"
)

type Dump struct {
	ID         int64
	DatabaseID int64
	Engine     string
	Label      string
	Storage    string
	DestID     int64
	Location   string
	SourceDB   string
	SizeBytes  int64
	Status     string
	CreatedBy  int64
	CreatedAt  time.Time
}

type DumpRow struct {
	Dump
	DatabaseName string
	DestName     string
}

const dumpCols = `id, database_id, engine, label, storage, location, source_db, size_bytes, status, created_by, created_at, dest_id`

func scanDump(row rowScanner) (*Dump, error) {
	var d Dump
	var created string
	err := row.Scan(&d.ID, &d.DatabaseID, &d.Engine, &d.Label, &d.Storage, &d.Location,
		&d.SourceDB, &d.SizeBytes, &d.Status, &d.CreatedBy, &created, &d.DestID)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}

func (s *Store) CreateDump(ctx context.Context, d *Dump) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO dumps
		(database_id, engine, label, storage, dest_id, location, source_db, size_bytes, status, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.DatabaseID, d.Engine, d.Label, d.Storage, d.DestID, d.Location, d.SourceDB, d.SizeBytes, d.Status, d.CreatedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetDump(ctx context.Context, id int64) (*Dump, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+dumpCols+` FROM dumps WHERE id = ?`, id)
	return scanDump(row)
}

func (s *Store) ListDumps(ctx context.Context) ([]DumpRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.database_id, d.engine, d.label, d.storage, d.location,
		d.source_db, d.size_bytes, d.status, d.created_by, d.created_at, d.dest_id,
		IFNULL(db.name, ''),
		CASE WHEN d.dest_id = 0 THEN 'local' ELSE IFNULL(sd.name, '') END
		FROM dumps d
		LEFT JOIN databases db ON db.id = d.database_id
		LEFT JOIN storage_destinations sd ON sd.id = d.dest_id
		ORDER BY d.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DumpRow
	for rows.Next() {
		var r DumpRow
		var created string
		err := rows.Scan(&r.ID, &r.DatabaseID, &r.Engine, &r.Label, &r.Storage, &r.Location,
			&r.SourceDB, &r.SizeBytes, &r.Status, &r.CreatedBy, &created, &r.DestID,
			&r.DatabaseName, &r.DestName)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateDumpResult(ctx context.Context, id int64, status, location string, size int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE dumps SET status = ?, location = ?, size_bytes = ? WHERE id = ?`, status, location, size, id)
	return err
}

func (s *Store) FailPendingDumps(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dumps SET status = 'failed' WHERE status = 'pending'`)
	return err
}

func (s *Store) DeleteDump(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dumps WHERE id = ?`, id)
	return err
}
