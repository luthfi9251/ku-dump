package meta

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Job struct {
	ID         int64
	Type       string
	DatabaseID int64
	DumpID     *int64
	Storage    *string
	Status     string
	LogPath    string
	Err        string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

type JobRow struct {
	Job
	DatabaseName string
	DumpLabel    string
}

func scanJob(row rowScanner) (*Job, error) {
	var j Job
	var created string
	var dumpID sql.NullInt64
	var storage sql.NullString
	var started, finished sql.NullString
	err := row.Scan(&j.ID, &j.Type, &j.DatabaseID, &dumpID, &storage, &j.Status,
		&j.LogPath, &j.Err, &created, &started, &finished)
	if err != nil {
		return nil, err
	}
	j.CreatedAt = parseTime(created)
	j.StartedAt = parseTimePtr(started)
	j.FinishedAt = parseTimePtr(finished)
	if dumpID.Valid {
		v := dumpID.Int64
		j.DumpID = &v
	}
	if storage.Valid {
		v := storage.String
		j.Storage = &v
	}
	return &j, nil
}

const jobCols = `id, type, database_id, dump_id, storage, status, log_path, error, created_at, started_at, finished_at`

func (s *Store) CreateJob(ctx context.Context, j *Job) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO jobs (type, database_id, dump_id, storage)
		VALUES (?, ?, ?, ?)`, j.Type, j.DatabaseID, j.DumpID, j.Storage)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

var ErrJobActive = errors.New("a job is already active for this database")

func (s *Store) CreateJobGuarded(ctx context.Context, j *Job) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO jobs (type, database_id, dump_id, storage)
		SELECT ?, ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM jobs WHERE database_id = ? AND status IN ('pending','running'))`,
		j.Type, j.DatabaseID, j.DumpID, j.Storage, j.DatabaseID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrJobActive
	}
	return res.LastInsertId()
}

func (s *Store) GetJob(ctx context.Context, id int64) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) ListJobs(ctx context.Context, limit int) ([]JobRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT j.id, j.type, j.database_id, j.dump_id, j.storage, j.status,
		j.log_path, j.error, j.created_at, j.started_at, j.finished_at,
		IFNULL(db.name, ''), IFNULL(dp.label, '')
		FROM jobs j
		LEFT JOIN databases db ON db.id = j.database_id
		LEFT JOIN dumps dp ON dp.id = j.dump_id
		ORDER BY j.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobRow
	for rows.Next() {
		var r JobRow
		var created string
		var dumpID sql.NullInt64
		var storage sql.NullString
		var started, finished sql.NullString
		err := rows.Scan(&r.ID, &r.Type, &r.DatabaseID, &dumpID, &storage, &r.Status,
			&r.LogPath, &r.Err, &created, &started, &finished, &r.DatabaseName, &r.DumpLabel)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		r.StartedAt = parseTimePtr(started)
		r.FinishedAt = parseTimePtr(finished)
		if dumpID.Valid {
			v := dumpID.Int64
			r.DumpID = &v
		}
		if storage.Valid {
			v := storage.String
			r.Storage = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SetJobRunning(ctx context.Context, id int64, logPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'running', log_path = ?, started_at = datetime('now') WHERE id = ?`, logPath, id)
	return err
}

func (s *Store) SetJobFinished(ctx context.Context, id int64, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, error = ?, finished_at = datetime('now') WHERE id = ?`, status, errMsg, id)
	return err
}

func (s *Store) HasActiveJob(ctx context.Context, databaseID int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM jobs WHERE database_id = ? AND status IN ('pending','running') LIMIT 1`, databaseID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) FailActiveJobs(ctx context.Context, msg string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'failed', error = ?, finished_at = datetime('now')
		 WHERE status IN ('pending','running')`, msg)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
