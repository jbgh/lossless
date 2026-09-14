package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// Snapshot writes a consistent single-file copy of the sqlite database at
// src to dst with VACUUM INTO on a fresh read connection. The daemon's
// WAL stays live and is not checkpointed. dst is replaced if present.
func Snapshot(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	db, err := sql.Open("sqlite", sqliteURI(src))
	if err != nil {
		return err
	}
	defer db.Close()
	quoted := "'" + strings.ReplaceAll(dst, "'", "''") + "'"
	if _, err := db.Exec("VACUUM INTO " + quoted); err != nil {
		return fmt.Errorf("snapshot %s: %w", src, err)
	}
	return os.Chmod(dst, 0o600)
}
