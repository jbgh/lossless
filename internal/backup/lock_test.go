package backup

import (
	"errors"
	"testing"
)

func TestLockIsExclusive(t *testing.T) {
	home := t.TempDir()
	release, err := Lock(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(home); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	release()
	release2, err := Lock(home)
	if err != nil {
		t.Fatal(err)
	}
	release2()
}
