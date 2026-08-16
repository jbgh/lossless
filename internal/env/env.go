package env

import (
	"os"
	"path/filepath"
)

func Home() string {
	if h := os.Getenv("LOSSLESS_HOME"); h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".lossless")
}

func URL() string {
	return os.Getenv("LOSSLESS_URL")
}

func Token() string {
	return os.Getenv("LOSSLESS_TOKEN")
}

func Sidecar() string {
	if u := os.Getenv("LOSSLESS_SIDECAR"); u != "" {
		return u
	}
	return "http://127.0.0.1:7432"
}

func Client() string {
	return os.Getenv("LOSSLESS_CLIENT")
}
