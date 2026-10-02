package meta

import (
	"context"
	"time"
)

type Destination struct {
	ID        int64
	Name      string
	Kind      string // "s3" | "local" | "sftp"
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretEnc string
	RootPath  string // local
	Host      string // sftp
	Port      int    // sftp
	Username  string // sftp
	AuthType  string // sftp: "" | "password" | "key"
	RemoteDir string // sftp
	CreatedAt time.Time
}

const destinationCols = `id, name, kind, endpoint, region, bucket, prefix, access_key, secret_enc,
	root_path, host, port, username, auth_type, remote_dir, created_at`

func scanDestination(row rowScanner) (*Destination, error) {
	var d Destination
	var created string
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.Endpoint, &d.Region, &d.Bucket,
		&d.Prefix, &d.AccessKey, &d.SecretEnc, &d.RootPath, &d.Host, &d.Port,
		&d.Username, &d.AuthType, &d.RemoteDir, &created)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}

func (s *Store) CreateDestination(ctx context.Context, d *Destination) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO storage_destinations
		(name, kind, endpoint, region, bucket, prefix, access_key, secret_enc,
		 root_path, host, port, username, auth_type, remote_dir)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.Kind, d.Endpoint, d.Region, d.Bucket, d.Prefix, d.AccessKey, d.SecretEnc,
		d.RootPath, d.Host, d.Port, d.Username, d.AuthType, d.RemoteDir)
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
		name = ?, kind = ?, endpoint = ?, region = ?, bucket = ?, prefix = ?, access_key = ?, secret_enc = ?,
		root_path = ?, host = ?, port = ?, username = ?, auth_type = ?, remote_dir = ?
		WHERE id = ?`,
		d.Name, d.Kind, d.Endpoint, d.Region, d.Bucket, d.Prefix, d.AccessKey, d.SecretEnc,
		d.RootPath, d.Host, d.Port, d.Username, d.AuthType, d.RemoteDir, d.ID)
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

// MigrateLegacyS3 moves the pre-multi-destination single S3 settings into a
// storage destination. Runs inside one transaction; a failure leaves the
// trigger key (s3_endpoint) in place so the next startup retries.
func (s *Store) MigrateLegacyS3(ctx context.Context) (bool, error) {
	ep, ok, err := s.GetSetting(ctx, "s3_endpoint")
	if err != nil || !ok {
		return false, err
	}
	get := func(k string) string {
		v, _, _ := s.GetSetting(ctx, k)
		return v
	}
	name := get("s3_bucket")
	if name == "" {
		name = "s3"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO storage_destinations
		(name, kind, endpoint, region, bucket, prefix, access_key, secret_enc)
		VALUES (?, 's3', ?, ?, ?, ?, ?, ?)`,
		name, ep, get("s3_region"), name, get("s3_prefix"), get("s3_access_key"), get("s3_secret_enc"))
	if err != nil {
		return false, err
	}
	destID, err := res.LastInsertId()
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dumps SET dest_id = ? WHERE storage = 's3'`, destID); err != nil {
		return false, err
	}
	for _, k := range []string{"s3_region", "s3_bucket", "s3_prefix", "s3_access_key", "s3_secret_enc", "s3_endpoint"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, k); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}
