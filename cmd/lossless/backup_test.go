// cmd/lossless/backup_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/backup/s3/s3test"
	"lossless/internal/store"
)

func TestBackupInitRunRestoreList(t *testing.T) {
	for _, k := range []string{"LOSSLESS_BACKUP_URL", "LOSSLESS_BACKUP_ENDPOINT", "LOSSLESS_BACKUP_EVERY", "LOSSLESS_BACKUP_KEEP", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "LOSSLESS_CLIENT"} {
		t.Setenv(k, "")
	}
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "test")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "secret")
	t.Setenv("LOSSLESS_SIDECAR", "off")
	srv := s3test.New()
	defer srv.Close()

	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	if runBackup([]string{"init"}) != 2 {
		t.Fatal("init without a URL must exit 2")
	}
	if runBackup([]string{"init", "--home", home, "--endpoint", srv.URL(), "--every", "30m", "--keep", "3", "s3://bkt/pre"}) != 0 {
		t.Fatal("init")
	}
	for _, f := range []string{"backup.env", "backup.key"} {
		if _, err := os.Stat(filepath.Join(home, f)); err != nil {
			t.Fatal(err)
		}
	}
	if runBackup([]string{"init", "--home", home, "s3://bkt/pre"}) != 1 {
		t.Fatal("second init must refuse to replace the key")
	}
	if runBackup([]string{"--home", home, "--dry-run"}) != 0 {
		t.Fatal("dry run")
	}
	if srv.Count("PUT") != 0 {
		t.Fatal("dry run wrote")
	}
	if runBackup([]string{"--home", home}) != 0 {
		t.Fatal("backup")
	}
	if srv.Count("PUT") == 0 {
		t.Fatal("nothing uploaded")
	}
	if runRestore([]string{"--home", home, "--list"}) != 0 {
		t.Fatal("list")
	}
	fresh := t.TempDir()
	st2, err := store.Open(fresh)
	if err != nil {
		t.Fatal(err)
	}
	_ = st2.Close()
	for _, f := range []string{"backup.env", "backup.key"} {
		b, _ := os.ReadFile(filepath.Join(home, f))
		if err := os.WriteFile(filepath.Join(fresh, f), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if runRestore([]string{"--home", fresh}) != 0 {
		t.Fatal("restore")
	}
	if _, err := os.Stat(filepath.Join(fresh, "index", "claims.sqlite")); err != nil {
		t.Fatal(err)
	}
	if runRestore([]string{"--home", fresh, "--at", "nope"}) != 1 {
		t.Fatal("unknown generation must exit 1")
	}
	if runBackup([]string{"--home", t.TempDir()}) != 2 {
		t.Fatal("unconfigured home must exit 2")
	}
	if runBackup([]string{"-bogus"}) != 2 || runRestore([]string{"-bogus"}) != 2 {
		t.Fatal("bad flags must exit 2")
	}
}
