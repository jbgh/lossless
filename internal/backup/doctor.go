// internal/backup/doctor.go
package backup

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DoctorCheck is the one line `lossless doctor` prints for backup.
func DoctorCheck(home string, now time.Time) (ok bool, detail string) {
	cfg, err := LoadConfig(home)
	if errors.Is(err, ErrNotConfigured) {
		return true, "not configured"
	}
	if err != nil {
		return false, err.Error()
	}
	st := LoadState(home)
	var b strings.Builder
	fmt.Fprintf(&b, "%s encrypted keep=%d", cfg.URL, cfg.Keep)
	if cfg.Every <= 0 {
		b.WriteString(" schedule off")
	}
	last, has := st.LastOKTime()
	ok = true
	if !has {
		b.WriteString(" never backed up")
		ok = false
		if cfg.Every <= 0 {
			b.WriteString(" (run lossless backup)")
		}
	} else {
		age := now.Sub(last)
		fmt.Fprintf(&b, " last ok %s ago", age.Round(time.Minute))
		if cfg.Every > 0 && age > 2*cfg.Every {
			b.WriteString(" (stale)")
			ok = false
		}
	}
	if cfg.Every > 0 {
		if !has {
			fmt.Fprintf(&b, ", due within %s of serve start", startFloor)
		} else if due := last.Add(cfg.Every); due.After(now) {
			fmt.Fprintf(&b, ", due in %s", due.Sub(now).Round(time.Minute))
		} else {
			b.WriteString(", due now")
		}
	}
	if st.LastError != "" && st.LastAttempt > st.LastOK {
		fmt.Fprintf(&b, "; last attempt failed: %s", st.LastError)
		ok = false
	}
	return ok, b.String()
}
