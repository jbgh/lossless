// internal/backup/doctor.go
package backup

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// FormatDuration renders a duration the way backup's user-facing messages
// want it, instead of Go's default "1h0m0s": whole hours as "1h", hours plus
// minutes as "1h30m", whole minutes as "12m", under a minute as "45s", and
// zero (or negative) as "0s". It does not round; callers that want a duration
// rounded to a whole minute or second do that first.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

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
		fmt.Fprintf(&b, " last ok %s ago", FormatDuration(age.Round(time.Minute)))
		if cfg.Every > 0 && age > 2*cfg.Every {
			b.WriteString(" (stale)")
			ok = false
		}
	}
	if cfg.Every > 0 {
		if !has {
			fmt.Fprintf(&b, ", due within %s of serve start", FormatDuration(startFloor))
		} else if due := last.Add(cfg.Every); due.After(now) {
			fmt.Fprintf(&b, ", due in %s", FormatDuration(due.Sub(now).Round(time.Minute)))
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
