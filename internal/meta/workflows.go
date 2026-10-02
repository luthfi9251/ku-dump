package meta

import (
	"context"
	"database/sql"
	"time"
)

const timeLayout = "2006-01-02 15:04:05"

type Workflow struct {
	ID          int64
	Name        string
	DatabaseID  int64
	DestID      int64
	TriggerKind string // "manual" | "once" | "cron"
	RunAt       *time.Time
	Cron        string
	Enabled     bool
	LastRunAt   *time.Time
	LastError   string
	NextRunAt   *time.Time
	CreatedAt   time.Time
}

type WorkflowRow struct {
	Workflow
	DatabaseName   string
	DatabaseEngine string
	DestName       string
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeLayout)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanWorkflow(row rowScanner) (*Workflow, error) {
	var wf Workflow
	var created string
	var runAt, lastRun, nextRun sql.NullString
	err := row.Scan(&wf.ID, &wf.Name, &wf.DatabaseID, &wf.DestID, &wf.TriggerKind,
		&runAt, &wf.Cron, &wf.Enabled, &lastRun, &wf.LastError, &nextRun, &created)
	if err != nil {
		return nil, err
	}
	wf.CreatedAt = parseTime(created)
	wf.RunAt = parseTimePtr(runAt)
	wf.LastRunAt = parseTimePtr(lastRun)
	wf.NextRunAt = parseTimePtr(nextRun)
	return &wf, nil
}

const workflowCols = `id, name, database_id, dest_id, trigger_kind, run_at, cron, enabled, last_run_at, last_error, next_run_at, created_at`

func (s *Store) CreateWorkflow(ctx context.Context, wf *Workflow) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO dump_workflows
		(name, database_id, dest_id, trigger_kind, run_at, cron, enabled, last_run_at, last_error, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		wf.Name, wf.DatabaseID, wf.DestID, wf.TriggerKind,
		formatTimePtr(wf.RunAt), wf.Cron, boolInt(wf.Enabled), formatTimePtr(wf.LastRunAt),
		wf.LastError, formatTimePtr(wf.NextRunAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetWorkflow(ctx context.Context, id int64) (*Workflow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+workflowCols+` FROM dump_workflows WHERE id = ?`, id)
	return scanWorkflow(row)
}

func (s *Store) ListWorkflows(ctx context.Context) ([]WorkflowRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id, w.name, w.database_id, w.dest_id, w.trigger_kind,
		w.run_at, w.cron, w.enabled, w.last_run_at, w.last_error, w.next_run_at, w.created_at,
		IFNULL(db.name, ''), IFNULL(db.engine, ''),
		CASE WHEN w.dest_id = 0 THEN 'local' ELSE IFNULL(sd.name, '') END
		FROM dump_workflows w
		LEFT JOIN databases db ON db.id = w.database_id
		LEFT JOIN storage_destinations sd ON sd.id = w.dest_id
		ORDER BY w.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowRow
	for rows.Next() {
		var r WorkflowRow
		var created string
		var runAt, lastRun, nextRun sql.NullString
		err := rows.Scan(&r.ID, &r.Name, &r.DatabaseID, &r.DestID, &r.TriggerKind,
			&runAt, &r.Cron, &r.Enabled, &lastRun, &r.LastError, &nextRun, &created,
			&r.DatabaseName, &r.DatabaseEngine, &r.DestName)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		r.RunAt = parseTimePtr(runAt)
		r.LastRunAt = parseTimePtr(lastRun)
		r.NextRunAt = parseTimePtr(nextRun)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateWorkflow(ctx context.Context, wf *Workflow) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dump_workflows SET
		name = ?, database_id = ?, dest_id = ?, trigger_kind = ?, run_at = ?, cron = ?,
		enabled = ?, last_run_at = ?, last_error = ?, next_run_at = ?
		WHERE id = ?`,
		wf.Name, wf.DatabaseID, wf.DestID, wf.TriggerKind,
		formatTimePtr(wf.RunAt), wf.Cron, boolInt(wf.Enabled),
		formatTimePtr(wf.LastRunAt), wf.LastError, formatTimePtr(wf.NextRunAt), wf.ID)
	return err
}

func (s *Store) DeleteWorkflow(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dump_workflows WHERE id = ?`, id)
	return err
}

func (s *Store) DueWorkflows(ctx context.Context, now time.Time) ([]Workflow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workflowCols+` FROM dump_workflows
		WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		ORDER BY id`, now.UTC().Format(timeLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		wf, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wf)
	}
	return out, rows.Err()
}

// SetWorkflowState updates run bookkeeping. lastRunAt nil keeps the current
// value; nextRunAt nil clears the schedule (manual/disabled).
func (s *Store) SetWorkflowState(ctx context.Context, id int64, lastRunAt *time.Time, lastErr string, enabled bool, nextRunAt *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dump_workflows SET
		last_run_at = COALESCE(?, last_run_at),
		last_error = ?,
		enabled = ?,
		next_run_at = ?
		WHERE id = ?`,
		formatTimePtr(lastRunAt), lastErr, boolInt(enabled), formatTimePtr(nextRunAt), id)
	return err
}

func (s *Store) CountWorkflowsForDestination(ctx context.Context, destID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dump_workflows WHERE dest_id = ?`, destID).Scan(&n)
	return n, err
}
