package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearBackupEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"LOSSLESS_BACKUP_URL", "LOSSLESS_BACKUP_ENDPOINT", "LOSSLESS_BACKUP_REGION",
		"LOSSLESS_BACKUP_ACCESS_KEY", "LOSSLESS_BACKUP_SECRET_KEY", "LOSSLESS_BACKUP_EVERY", "LOSSLESS_BACKUP_KEEP",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(k, "")
	}
}

func TestLoadConfigNotConfigured(t *testing.T) {
	clearBackupEnv(t)
	_, err := LoadConfig(t.TempDir())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestInitThenLoad(t *testing.T) {
	clearBackupEnv(t)
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "AK")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "SK")
	home := t.TempDir()
	err := Init(home, InitOptions{URL: "s3://bkt/pre", Endpoint: "https://acct.r2.cloudflarestorage.com", Every: time.Hour, Keep: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"backup.env", "backup.key"} {
		st, err := os.Stat(filepath.Join(home, f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v mode=%v", f, err, st.Mode())
		}
	}
	// Credentials present at init time are written into backup.env so the
	// daemon can read them later without the shell environment.
	clearBackupEnv(t)
	cfg, err := LoadConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.Bucket != "bkt" || cfg.S3.Prefix != "pre" || cfg.S3.Endpoint != "https://acct.r2.cloudflarestorage.com" ||
		cfg.S3.AccessKey != "AK" || cfg.S3.SecretKey != "SK" || cfg.Every != time.Hour || cfg.Keep != 5 {
		t.Fatalf("%+v", cfg)
	}
	if _, err := LoadKey(home); err != nil {
		t.Fatal(err)
	}
	// A second init must not overwrite the key.
	if err := Init(home, InitOptions{URL: "s3://other"}); err == nil || !strings.Contains(err.Error(), "backup.key") {
		t.Fatalf("second init must refuse: %v", err)
	}
}

func TestInitWithoutCredentialsLeavesPlaceholders(t *testing.T) {
	clearBackupEnv(t)
	home := t.TempDir()
	if err := Init(home, InitOptions{URL: "s3://bkt"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "backup.env"))
	if !strings.Contains(string(b), "# LOSSLESS_BACKUP_ACCESS_KEY=") {
		t.Fatalf("placeholder missing:\n%s", b)
	}
	_, err := LoadConfig(home)
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("want credentials error, got %v", err)
	}
	// AWS_* in the environment is an accepted fallback.
	t.Setenv("AWS_ACCESS_KEY_ID", "A")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "S")
	t.Setenv("AWS_SESSION_TOKEN", "T")
	cfg, err := LoadConfig(home)
	if err != nil || cfg.S3.AccessKey != "A" || cfg.S3.SessionToken != "T" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestInitDefaultsAndDisabledSchedule(t *testing.T) {
	clearBackupEnv(t)
	home := t.TempDir()
	if err := Init(home, InitOptions{URL: "s3://bkt", Every: 0}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "backup.env"))
	if !strings.Contains(string(b), `LOSSLESS_BACKUP_EVERY="0"`) || !strings.Contains(string(b), `LOSSLESS_BACKUP_KEEP="5"`) {
		t.Fatalf("defaults:\n%s", b)
	}
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "A")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "S")
	cfg, err := LoadConfig(home)
	if err != nil || cfg.Every != 0 || cfg.Keep != 5 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if err := Init(t.TempDir(), InitOptions{URL: "nope"}); err == nil {
		t.Fatal("bad URL must be refused")
	}
}

func TestLoadConfigMalformedQuotedValueErrors(t *testing.T) {
	clearBackupEnv(t)
	home := t.TempDir()
	content := "LOSSLESS_BACKUP_URL=\"s3://bkt\"\nLOSSLESS_BACKUP_SECRET_KEY=\"unterminated\n"
	if err := os.WriteFile(filepath.Join(home, "backup.env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "AK")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "SK")
	_, err := LoadConfig(home)
	if err == nil || !strings.Contains(err.Error(), "LOSSLESS_BACKUP_SECRET_KEY") {
		t.Fatalf("want error mentioning LOSSLESS_BACKUP_SECRET_KEY, got %v", err)
	}
}
