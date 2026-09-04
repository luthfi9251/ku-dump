package meta

import (
	"context"
	"errors"
	"testing"
)

func TestMigrateLegacyS3(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	st.SetSetting(ctx, "s3_endpoint", "http://127.0.0.1:9000")
	st.SetSetting(ctx, "s3_region", "us-east-1")
	st.SetSetting(ctx, "s3_bucket", "oldbucket")
	st.SetSetting(ctx, "s3_prefix", "pre")
	st.SetSetting(ctx, "s3_access_key", "AK")
	st.SetSetting(ctx, "s3_secret_enc", "ENCRYPTED")
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "old", Storage: "s3", Status: "ready", CreatedBy: 1})
	st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "loc", Storage: "local", Status: "ready", CreatedBy: 1})

	migrated, err := st.MigrateLegacyS3(ctx)
	if err != nil || !migrated {
		t.Fatalf("migrated = %v, %v", migrated, err)
	}
	dest, err := st.GetDestinationByName(ctx, "oldbucket")
	if err != nil {
		t.Fatal(err)
	}
	if dest.Endpoint != "http://127.0.0.1:9000" || dest.Region != "us-east-1" || dest.Prefix != "pre" ||
		dest.AccessKey != "AK" || dest.SecretEnc != "ENCRYPTED" || dest.Kind != "s3" {
		t.Fatalf("dest = %+v", dest)
	}
	rows, _ := st.ListDumps(ctx)
	for _, r := range rows {
		want := dest.ID
		if r.Label == "loc" {
			want = 0
		}
		if r.DestID != want {
			t.Fatalf("dump %s dest = %d, want %d", r.Label, r.DestID, want)
		}
	}
	for _, k := range []string{"s3_endpoint", "s3_region", "s3_bucket", "s3_prefix", "s3_access_key", "s3_secret_enc"} {
		if _, ok, _ := st.GetSetting(ctx, k); ok {
			t.Fatalf("setting %s not deleted", k)
		}
	}
	if again, err := st.MigrateLegacyS3(ctx); err != nil || again {
		t.Fatalf("second run = %v, %v", again, err)
	}
}

func TestDestinationCRUD(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	if rows, _ := st.ListDestinations(ctx); len(rows) != 0 {
		t.Fatalf("initial len = %d", len(rows))
	}
	id, err := st.CreateDestination(ctx, &Destination{Name: "minio", Kind: "s3",
		Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", Bucket: "ku",
		Prefix: "backups", AccessKey: "ak", SecretEnc: "ENC"})
	if err != nil || id == 0 {
		t.Fatalf("create = %d, %v", id, err)
	}
	got, err := st.GetDestination(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "minio" || got.Kind != "s3" || got.Endpoint != "http://127.0.0.1:9000" ||
		got.Region != "us-east-1" || got.Bucket != "ku" || got.Prefix != "backups" ||
		got.AccessKey != "ak" || got.SecretEnc != "ENC" || got.CreatedAt.IsZero() {
		t.Fatalf("got = %+v", got)
	}
	byName, err := st.GetDestinationByName(ctx, "minio")
	if err != nil || byName.ID != id {
		t.Fatalf("byName = %+v, %v", byName, err)
	}
	if _, err := st.GetDestinationByName(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	got.Prefix = "other"
	if err := st.UpdateDestination(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := st.GetDestination(ctx, id)
	if again.Prefix != "other" {
		t.Fatalf("update not applied: %+v", again)
	}
	if _, err := st.CreateDestination(ctx, &Destination{Name: "minio", Kind: "s3",
		Endpoint: "http://x", Bucket: "b", AccessKey: "a", SecretEnc: "s"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	rows, _ := st.ListDestinations(ctx)
	if len(rows) != 1 {
		t.Fatalf("len = %d", len(rows))
	}
	if err := st.DeleteDestination(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDestination(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
