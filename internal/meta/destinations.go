package meta

import (
	"context"
	"time"
)

type Destination struct {
	ID        int64
	Name      string
	Kind      string
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretEnc string
	CreatedAt time.Time
}

const destinationCols = `id, name, kind, endpoint, region, bucket, prefix, access_key, secret_enc, created_at`

func scanDestination(row rowScanner) (*Destination, error) {
	var d Destination
	var created string
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.Endpoint, &d.Region, &d.Bucket,
		&d.Prefix, &d.AccessKey, &d.SecretEnc, &created)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}

func (s *Store) CreateDestination(ctx context.Context, d *Destination) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO storage_destinations
		(name, kind, endpoint, region, bucket, prefix, access_key, secret_enc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.Kind, d.Endpoint, d.Region, d.Bucket, d.Prefix, d.AccessKey, d.SecretEnc)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListDestinations(ctx context.Context) ([]Destination, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+destinationCols+` FROM storage_destinations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destination
	for rows.Next() {
		d, err := scanDestination(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) GetDestination(ctx context.Context, id int64) (*Destination, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+destinationCols+` FROM storage_destinations WHERE id = ?`, id)
	return scanDestination(row)
}

func (s *Store) GetDestinationByName(ctx context.Context, name string) (*Destination, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+destinationCols+` FROM storage_destinations WHERE name = ?`, name)
	return scanDestination(row)
}

func (s *Store) UpdateDestination(ctx context.Context, d *Destination) error {
	_, err := s.db.ExecContext(ctx, `UPDATE storage_destinations SET
		name = ?, kind = ?, endpoint = ?, region = ?, bucket = ?, prefix = ?, access_key = ?, secret_enc = ?
		WHERE id = ?`,
		d.Name, d.Kind, d.Endpoint, d.Region, d.Bucket, d.Prefix, d.AccessKey, d.SecretEnc, d.ID)
	return err
}

func (s *Store) DeleteDestination(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM storage_destinations WHERE id = ?`, id)
	return err
}

func (s *Store) CountDumpsForDestination(ctx context.Context, id int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dumps WHERE dest_id = ?`, id).Scan(&n)
	return n, err
}
