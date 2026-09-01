package write

import (
	"testing"

	"lossless/internal/claim"
)

func TestRememberRejectsSecretInWhyAndSymbols(t *testing.T) {
	st := tmpStore(t)
	if _, err := Remember(st, claim.Record{
		Type: "decision", ProjectKey: "acme/api",
		Text: "rotate the deploy token", Why: "old token was ghp_abcdefghijklmnopqrstuvwxyz123456",
	}); err == nil {
		t.Fatal("secret in why accepted")
	}
	if _, err := Remember(st, claim.Record{
		Type: "decision", ProjectKey: "acme/api",
		Text: "rotate the deploy token", Symbols: []string{"ghp_abcdefghijklmnopqrstuvwxyz123456"},
	}); err == nil {
		t.Fatal("secret in symbols accepted")
	}
}
