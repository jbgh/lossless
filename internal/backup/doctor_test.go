// internal/backup/doctor_test.go
package backup

import (
	"strings"
	"testing"
	"time"
)

func TestDoctorCheck(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clearBackupEnv(t)
	if ok, d := DoctorCheck(t.TempDir(), now); !ok || d != "not configured" {
		t.Fatal(ok, d)
	}
	home := scheduledHome(t, time.Hour, "")
	if ok, d := DoctorCheck(home, now); ok || !strings.Contains(d, "never") {
		t.Fatal(ok, d)
	}
	st := LoadState(home)
	st.LastOK = now.Add(-12 * time.Minute).Format(time.RFC3339)
	must(t, st.Save(home))
	ok, d := DoctorCheck(home, now)
	if !ok || !strings.Contains(d, "s3://b") || !strings.Contains(d, "encrypted") || !strings.Contains(d, "keep=5") || !strings.Contains(d, "12m") || !strings.Contains(d, "due in 48m") {
		t.Fatal(ok, d)
	}
	st.LastOK = now.Add(-3 * time.Hour).Format(time.RFC3339)
	must(t, st.Save(home))
	if ok, d := DoctorCheck(home, now); ok || !strings.Contains(d, "stale") {
		t.Fatal(ok, d)
	}
	st.LastOK = now.Add(-12 * time.Minute).Format(time.RFC3339)
	st.LastAttempt = now.Add(-2 * time.Minute).Format(time.RFC3339)
	st.LastError = "s3 PUT manifest: 503"
	must(t, st.Save(home))
	if ok, d := DoctorCheck(home, now); ok || !strings.Contains(d, "last attempt failed: s3 PUT manifest: 503") {
		t.Fatal(ok, d)
	}
	// Schedule off: only "never" is a failure.
	off := scheduledHome(t, 0, now.Add(-30*24*time.Hour).Format(time.RFC3339))
	if ok, d := DoctorCheck(off, now); !ok || !strings.Contains(d, "schedule off") {
		t.Fatal(ok, d)
	}
}
