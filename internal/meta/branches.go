package meta

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Branches split the history of one database the way git branches split a
// repository. Every dump belongs to the branch that was active on its
// database when it started. A successful restore of dump X into database D
// either continues an existing branch (X is the newest dump of a branch of D)
// or starts a new one (X is an older point of D, or comes from another
// database or an upload). Branch names are random until the user renames them.

type Branch struct {
	ID         int64
	DatabaseID int64
	Name       string
	CreatedAt  time.Time
}

var ErrBranchNameTaken = errors.New("branch name already used on this database")

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var branchAdjectives = []string{
	"amber", "brisk", "calm", "coral", "crisp", "dusky", "eager", "fern", "frosty", "gentle",
	"golden", "hazel", "ivory", "jolly", "keen", "lunar", "misty", "noble", "olive", "plucky",
	"quiet", "rapid", "rusty", "sandy", "silver", "sunny", "swift", "teal", "velvet", "witty",
}

var branchNouns = []string{
	"badger", "bison", "cedar", "comet", "crane", "delta", "ember", "falcon", "fjord", "gecko",
	"harbor", "heron", "island", "lynx", "maple", "meadow", "otter", "panda", "pebble", "quartz",
	"raven", "reef", "river", "sparrow", "summit", "tundra", "walrus", "willow", "yak", "zephyr",
}

func randomBranchName() string {
	return branchAdjectives[rand.IntN(len(branchAdjectives))] + "-" + branchNouns[rand.IntN(len(branchNouns))]
}

func createBranch(ctx context.Context, q querier, databaseID int64) (int64, error) {
	for i := 0; ; i++ {
		name := randomBranchName()
		if i >= 10 {
			name = fmt.Sprintf("%s-%d", name, rand.IntN(1000))
		}
		var taken int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM branches WHERE database_id = ? AND name = ?`, databaseID, name).Scan(&taken); err != nil {
			return 0, err
		}
		if taken > 0 {
			continue
		}
		res, err := q.ExecContext(ctx, `INSERT INTO branches (database_id, name) VALUES (?, ?)`, databaseID, name)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
}

// restoreTarget decides which branch a restore of dumpID into databaseID
// lands on, looking only at events before job id beforeJobID. It returns the
// existing branch to continue, or 0 when the restore starts a new branch.
func restoreTarget(ctx context.Context, q querier, dumpID, databaseID, beforeJobID int64) (int64, error) {
	var srcDB, branchID int64
	err := q.QueryRowContext(ctx, `SELECT database_id, branch_id FROM dumps WHERE id = ?`, dumpID).Scan(&srcDB, &branchID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil || srcDB != databaseID || branchID == 0 {
		return 0, err
	}
	var tip int64
	err = q.QueryRowContext(ctx, `SELECT d.id FROM dumps d
		JOIN jobs j ON j.dump_id = d.id AND j.type = 'dump'
		WHERE d.branch_id = ? AND d.status IN ('ready','uploaded') AND j.id < ?
		ORDER BY j.id DESC LIMIT 1`, branchID, beforeJobID).Scan(&tip)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if tip == dumpID {
		return branchID, nil
	}
	return 0, nil
}

// assignRestoreBranch records the branch a successful restore job landed on.
func assignRestoreBranch(ctx context.Context, q querier, jobID, dumpID, databaseID int64) (int64, error) {
	branchID, err := restoreTarget(ctx, q, dumpID, databaseID, jobID)
	if err != nil {
		return 0, err
	}
	if branchID == 0 {
		if branchID, err = createBranch(ctx, q, databaseID); err != nil {
			return 0, err
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE jobs SET branch_id = ? WHERE id = ?`, branchID, jobID)
	return branchID, err
}

// AssignRestoreBranch is called by the runner when a restore job succeeded,
// before the job is marked finished.
func (s *Store) AssignRestoreBranch(ctx context.Context, jobID int64) error {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.DumpID == nil {
		return nil
	}
	_, err = assignRestoreBranch(ctx, s.db, jobID, *job.DumpID, job.DatabaseID)
	return err
}

// PreviewRestoreBranch tells what a restore of dumpID into databaseID would
// do right now: continue the returned branch, or (nil) start a new one.
func (s *Store) PreviewRestoreBranch(ctx context.Context, dumpID, databaseID int64) (*Branch, error) {
	branchID, err := restoreTarget(ctx, s.db, dumpID, databaseID, int64(1)<<62)
	if err != nil || branchID == 0 {
		return nil, err
	}
	return s.GetBranch(ctx, branchID)
}

// dumpBranch returns the branch a new dump joins: the branch of the state it
// starts from, or a new branch when the database has no known state yet.
func dumpBranch(ctx context.Context, q querier, databaseID, parentDumpID, parentJobID int64) (int64, error) {
	var branchID int64
	var err error
	switch {
	case parentJobID != 0:
		err = q.QueryRowContext(ctx, `SELECT branch_id FROM jobs WHERE id = ?`, parentJobID).Scan(&branchID)
	case parentDumpID != 0:
		err = q.QueryRowContext(ctx, `SELECT branch_id FROM dumps WHERE id = ?`, parentDumpID).Scan(&branchID)
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if branchID != 0 {
		return branchID, nil
	}
	return createBranch(ctx, q, databaseID)
}

func scanBranch(row rowScanner) (*Branch, error) {
	var b Branch
	var created string
	if err := row.Scan(&b.ID, &b.DatabaseID, &b.Name, &created); err != nil {
		return nil, err
	}
	b.CreatedAt = parseTime(created)
	return &b, nil
}

func (s *Store) GetBranch(ctx context.Context, id int64) (*Branch, error) {
	return scanBranch(s.db.QueryRowContext(ctx, `SELECT id, database_id, name, created_at FROM branches WHERE id = ?`, id))
}

func (s *Store) ListBranches(ctx context.Context) ([]Branch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, database_id, name, created_at FROM branches ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Store) RenameBranch(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	b, err := s.GetBranch(ctx, id)
	if err != nil {
		return err
	}
	var taken int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM branches WHERE database_id = ? AND name = ? AND id != ?`,
		b.DatabaseID, name, id).Scan(&taken); err != nil {
		return err
	}
	if taken > 0 {
		return ErrBranchNameTaken
	}
	_, err = s.db.ExecContext(ctx, `UPDATE branches SET name = ? WHERE id = ?`, name, id)
	return err
}

// backfillBranches assigns branches to history recorded before branches
// existed, replaying each database's successful jobs in order.
func backfillBranches(db *sql.DB) error {
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `SELECT j.id, j.type, j.database_id, IFNULL(j.dump_id, 0) FROM jobs j
		LEFT JOIN dumps d ON d.id = j.dump_id
		WHERE j.status = 'success' AND (j.type = 'restore' OR d.status IN ('ready','uploaded'))
		ORDER BY j.id`)
	if err != nil {
		return err
	}
	type ev struct {
		jobID, dbID, dumpID int64
		typ                 string
	}
	var evs []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.jobID, &e.typ, &e.dbID, &e.dumpID); err != nil {
			rows.Close()
			return err
		}
		evs = append(evs, e)
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
	active := map[int64]int64{}
	for _, e := range evs {
		if e.typ == "restore" {
			b, err := assignRestoreBranch(ctx, tx, e.jobID, e.dumpID, e.dbID)
			if err != nil {
				return err
			}
			active[e.dbID] = b
			continue
		}
		if active[e.dbID] == 0 {
			b, err := createBranch(ctx, tx, e.dbID)
			if err != nil {
				return err
			}
			active[e.dbID] = b
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dumps SET branch_id = ? WHERE id = ?`, active[e.dbID], e.dumpID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
