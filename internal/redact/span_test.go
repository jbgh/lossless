package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// A secret blanks its own span, not the whole transcript line: on
// 2026-09-22 whole-line redaction dropped 163 lines (86 assistant turns)
// from six live sessions, 119 of them for the local dev URL
// postgres://memora:memora@, and the decisions in those turns with them.
func TestLineRedactsSpanKeepsTurn(t *testing.T) {
	in := `{"type":"assistant","message":{"content":[{"type":"text","text":"Connect with postgres://memora:memora@localhost:5432/memora first. We decided to use sqlc for queries."}]}}`
	got := Line(in)
	want := `{"type":"assistant","message":{"content":[{"type":"text","text":"Connect with postgres://memora:[redacted]@localhost:5432/memora first. We decided to use sqlc for queries."}]}}` + "\n"
	if got != want {
		t.Fatalf("Line =\n%s\nwant\n%s", got, want)
	}
}

func TestLineRedactsValueOfAssignment(t *testing.T) {
	got := Line(`{"text":"export JWT_SECRET=lbSkUUrsxEGX67GmrT9eZcAL8rHr and restart <api>"}`)
	if got != `{"text":"export JWT_SECRET=[redacted] and restart <api>"}`+"\n" {
		t.Fatalf("Line = %s", got)
	}
}

func TestLineRedactsPrivateKeyBlock(t *testing.T) {
	body := strings.Repeat("MIIEvQIBADANBgkqhkiG9w0BAQEFAASC", 4)
	for _, tc := range []struct{ name, text, keep, drop string }{
		{"terminated", "key:\n-----BEGIN PRIVATE KEY-----\n" + body + "\n-----END PRIVATE KEY-----\nthen restart the box", "then restart the box", body},
	} {
		b, _ := json.Marshal(map[string]string{"text": tc.text})
		got := Line(string(b))
		if ContainsSecret(got) || strings.Contains(got, tc.drop) {
			t.Errorf("%s: key survived: %s", tc.name, got)
		}
		if !strings.Contains(got, tc.keep) || strings.Contains(got, "_redacted") {
			t.Errorf("%s: turn dropped: %s", tc.name, got)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("%s: invalid JSON: %s", tc.name, got)
		}
	}
}

// The blank runs to the end of the token: a pattern's character class
// may stop inside a secret (review, 2026-09-22), and the tail must not
// reach the tape.
func TestLineBlanksWholeToken(t *testing.T) {
	for _, tc := range []struct{ in, tail string }{
		{`{"content":"export DB_PASSWORD=Sup3rS3cret!Tail#Rest now"}`, "Tail"},
		{`{"content":"curl -H 'Authorization: Bearer abcdefghijklmnopqrstuvwx+/yzSECRETTAIL==' x"}`, "SECRETTAIL"},
		{`{"content":"key sk-abcdefghijklmnopqrstuv-SECRETTAIL_xyz here"}`, "SECRETTAIL"},
	} {
		got := Line(tc.in)
		if strings.Contains(got, tc.tail) || !strings.Contains(got, "[redacted]") {
			t.Errorf("Line(%s) = %s", tc.in, got)
		}
	}
	if got := Line(`{"content":"export DB_PASSWORD=Sup3rS3cret!Tail#Rest now"}`); !strings.HasSuffix(got, " now\"}\n") {
		t.Errorf("text after the token must stay: %s", got)
	}
}

// A private key whose END is not in the same JSON string (one string per
// file line in a Write/Edit structuredPatch) drops the whole line: the
// body sits in strings the key regex never sees.
func TestLineDropsSplitPrivateKey(t *testing.T) {
	in := `{"structuredPatch":[{"lines":["+-----BEGIN RSA PRIVATE KEY-----","+MIIEpAIBAAKCAQEAsecretbody","+-----END RSA PRIVATE KEY-----"]}]}`
	if got := Line(in); strings.Contains(got, "secretbody") || !strings.Contains(got, "_redacted") {
		t.Fatalf("split key reached the tape: %s", got)
	}
}

// A line that is not JSON, or a secret the span pass cannot isolate,
// still drops whole.
func TestLineFallsBackToMarker(t *testing.T) {
	if got := Line(`not json AKIAIOSFODNN7EXAMPLE`); !strings.Contains(got, "_redacted") {
		t.Fatalf("non-JSON secret line kept: %s", got)
	}
}

// Placeholders, code references, secret names and paths are not
// credentials (all from redacted live lines on 2026-09-22).
func TestContainsSecretSkipsReferences(t *testing.T) {
	no := []string{
		"redis://:<password>@cache.internal:6379",
		"postgres://memora:<pw>@db.internal/memora",
		"postgres://app:${PGPASSWORD}@db/app",
		"from_secret: firebase_android_api_key",
		"from_secret: android_keystore_b64",
		"apiKey = process.env.OPENROUTER_API_KEY",
		"refreshToken = cookieStore.refreshToken",
		"password = base64.b64decode",
		"bearer_token:/etc/prometheus/backend-metrics-token",
		"git push origin HEAD:refs/for/main -o api-key:refs/for/main",
		"`--font-*` isn't a design token: next/font injects these",
		"ADMIN_METRICS_TOKEN: cmd/server reads it",
	}
	for _, s := range no {
		if ContainsSecret(s) {
			t.Errorf("ContainsSecret(%q) = true, want false", s)
		}
	}
	// Exemptions cover the reference shapes only: a weak real password
	// in snake_case, a passphrase, a $-leading password, a digit-bearing
	// dotted value, or a base64 value that opens with / still redacts.
	yes := []string{
		"export DB_PASSWORD=weak_password123",
		"password: correct-horse-battery-staple",
		"postgres://app:$ecretPass1@db/app",
		"password=hello.world2024",
		"AWS_SECRET_ACCESS_KEY=/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"api_key: firebase_android_api_key",
		"postgres://memora:memora@localhost/memora",
		"JWT_SECRET=lbSkUUrsxEGX67GmrT9eZcAL8rHr",
		"password: Sup3r_S3cret_2024",
	}
	for _, s := range yes {
		if !ContainsSecret(s) {
			t.Errorf("ContainsSecret(%q) = false, want true", s)
		}
	}
}
