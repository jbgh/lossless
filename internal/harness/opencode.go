package harness

import (
	"os"
	"path/filepath"
)

func OpenCodeHome() string {
	if h := os.Getenv("OPENCODE_HOME"); h != "" {
		return h
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(data, "opencode")
}

func OpenCodeDB() string {
	if p := os.Getenv("OPENCODE_DB"); p != "" {
		return p
	}
	return filepath.Join(OpenCodeHome(), "opencode.db")
}

func OpenCodeConfigDir() string {
	if h := os.Getenv("OPENCODE_CONFIG"); h != "" {
		return h
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(cfg, "opencode")
}

func LocateOpenCode(sessionID, cwd string) Locate {
	return Locate{SessionID: sessionID, CWD: cwd}
}
