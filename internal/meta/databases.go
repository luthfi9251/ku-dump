package meta

import (
	"context"
	"database/sql"
	"time"
)

type Database struct {
	ID          int64
	Name        string
	Engine      string
	Host        string
	Port        int
	DBName      string
	Username    string
	PasswordEnc string
	Options     string
	CreatedAt   time.Time
	LastTestAt  *time.Time
	LastTestOK  *bool
}

const databaseCols = `id, name, engine, host, port, db_name, username, password_enc, options, created_at, last_test_at, last_test_ok`

type rowScanner interface{ Scan(dest ...any) error }

func scanDatabase(row rowScanner) (*Database, error) {
	var d Database
	var created string
	var lastAt sql.NullString
	var lastOK sql.NullInt64
	err := row.Scan(&d.ID, &d.Name, &d.Engine, &d.Host, &d.Port, &d.DBName, &d.Username,
		&d.PasswordEnc, &d.Options, &created, &lastAt, &lastOK)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	d.LastTestAt = parseTimePtr(lastAt)
	if lastOK.Valid {
		v := lastOK.Int64 == 1
		d.LastTestOK = &v
	}
	return &d, nil
}

func orJSON(options string) string {
	if options == "" {
		return "{}"
	}
	return options
}

func (s *Store) CreateDatabase(ctx context.Context, d *Database) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO databases
		(name, engine, host, port, db_name, username, password_enc, options)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.Engine, d.Host, d.Port, d.DBName, d.Username, d.PasswordEnc, orJSON(d.Options))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+databaseCols+` FROM databases ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Database
	for rows.Next() {
		d, err := scanDatabase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) GetDatabase(ctx context.Context, id int64) (*Database, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+databaseCols+` FROM databases WHERE id = ?`, id)
	return scanDatabase(row)
}

func (s *Store) GetDatabaseByName(ctx context.Context, name string) (*Database, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+databaseCols+` FROM databases WHERE name = ?`, name)
	return scanDatabase(row)
}

func (s *Store) UpdateDatabase(ctx context.Context, d *Database) error {
	_, err := s.db.ExecContext(ctx, `UPDATE databases SET
		name = ?, engine = ?, host = ?, port = ?, db_name = ?, username = ?, password_enc = ?, options = ?
		WHERE id = ?`,
		d.Name, d.Engine, d.Host, d.Port, d.DBName, d.Username, d.PasswordEnc, orJSON(d.Options), d.ID)
	return err
}

func (s *Store) DeleteDatabase(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM databases WHERE id = ?`, id)
	return err
}

func (s *Store) SetDatabaseTestResult(ctx context.Context, id int64, ok bool) error {
	v := 0
	if ok {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE databases SET last_test_at = datetime('now'), last_test_ok = ? WHERE id = ?`, v, id)
	return err
}
