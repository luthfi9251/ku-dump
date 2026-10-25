package meta

import (
	"context"
	"database/sql"
	"math"
	"time"
)

// Lineage: every dump records the state its database was in when the dump
// started. That state is the latest successful event on the database: either
// a previous dump of it (ParentDumpID set, ParentJobID 0) or a restore into it
// (ParentJobID = the restore job, ParentDumpID = the restore's source dump).
// Jobs on one database never overlap (CreateJobGuarded), so job ids order the
// events of a database exactly.

// stateBefore returns the parent of an event on databaseID that happens at
// job id beforeJobID.
func stateBefore(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, databaseID, beforeJobID int64) (parentDumpID, parentJobID int64, err error) {
	var jobID int64
	var typ string
	var dumpID sql.NullInt64
	err = q.QueryRowContext(ctx, `SELECT j.id, j.type, j.dump_id FROM jobs j
		LEFT JOIN dumps d ON d.id = j.dump_id
		WHERE j.database_id = ? AND j.status = 'success' AND j.id < ?
		  AND (j.type = 'restore' OR d.status IN ('ready','uploaded'))
		ORDER BY j.id DESC LIMIT 1`, databaseID, beforeJobID).Scan(&jobID, &typ, &dumpID)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if typ == "restore" {
		return dumpID.Int64, jobID, nil
	}
	return dumpID.Int64, 0, nil
}

// SetDumpParent records the lineage of a dump whose job was just created.
// Called after CreateJobGuarded succeeds, when no other job can change the
// database's state any more.
func (s *Store) SetDumpParent(ctx context.Context, dumpID, databaseID, jobID int64) error {
	pd, pj, err := stateBefore(ctx, s.db, databaseID, jobID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE dumps SET parent_dump_id = ?, parent_job_id = ? WHERE id = ?`, pd, pj, dumpID)
	return err
}

// backfillLineage fills parent columns for dumps created before lineage was
// tracked, replaying the jobs history. Runs once, when the columns are added.
func backfillLineage(db *sql.DB) error {
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `SELECT d.id, d.database_id, IFNULL(MIN(j.id), ?) FROM dumps d
		LEFT JOIN jobs j ON j.dump_id = d.id AND j.type = 'dump'
		WHERE d.database_id != 0 GROUP BY d.id`, int64(math.MaxInt64))
	if err != nil {
		return err
	}
	type pending struct{ id, dbID, jobID int64 }
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.dbID, &p.jobID); err != nil {
			rows.Close()
			return err
		}
		list = append(list, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range list {
		if p.jobID == math.MaxInt64 {
			continue // dump without a job record: leave it as a root
		}
		pd, pj, err := stateBefore(ctx, tx, p.dbID, p.jobID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE dumps SET parent_dump_id = ?, parent_job_id = ? WHERE id = ?`, pd, pj, p.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type RestoreRow struct {
	JobID        int64
	DatabaseID   int64
	DatabaseName string
	Engine       string
	DumpID       int64
	DumpExists   bool
	Status       string
	CreatedAt    time.Time
	FinishedAt   *time.Time
}

// ListRestores returns every restore job with its target database and the
// engine of the target (or of the source dump when the target was deleted).
func (s *Store) ListRestores(ctx context.Context) ([]RestoreRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT j.id, j.database_id, IFNULL(db.name, ''),
		IFNULL(db.engine, IFNULL(dp.engine, '')), IFNULL(j.dump_id, 0), dp.id IS NOT NULL,
		j.status, j.created_at, j.finished_at
		FROM jobs j
		LEFT JOIN databases db ON db.id = j.database_id
		LEFT JOIN dumps dp ON dp.id = j.dump_id
		WHERE j.type = 'restore'
		ORDER BY j.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RestoreRow
	for rows.Next() {
		var r RestoreRow
		var created string
		var finished sql.NullString
		if err := rows.Scan(&r.JobID, &r.DatabaseID, &r.DatabaseName, &r.Engine, &r.DumpID, &r.DumpExists,
			&r.Status, &created, &finished); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		r.FinishedAt = parseTimePtr(finished)
		out = append(out, r)
	}
	return out, rows.Err()
}
