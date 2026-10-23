package redact

import (
	"bufio"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// containsSecretSlow is ContainsSecret before the literal prefilter: every
// regex over every text. The prefilter must never change its answer.
func containsSecretSlow(text string) bool {
	for _, re := range secrets {
		if re.MatchString(text) {
			return true
		}
	}
	for _, m := range dsnPassword.FindAllStringSubmatch(text, -1) {
		if !placeholder(m[2]) {
			return true
		}
	}
	for _, m := range secretAssign.FindAllStringSubmatch(text, -1) {
		if assignedSecret(m[0], m[1]) {
			return true
		}
	}
	return false
}

// lineSlow is Line with containsSecretSlow in both of its checks.
func lineSlow(line string) string {
	trim := strings.TrimSpace(line)
	if trim == "" {
		return line
	}
	if containsSecretSlow(line) {
		if out, ok := scrubJSONStringsSlow(strings.TrimRight(line, "\r\n")); ok {
			return out + "\n"
		}
		b, _ := json.Marshal(map[string]bool{"_redacted": true})
		return string(b) + "\n"
	}
	if !strings.HasSuffix(line, "\n") {
		return line + "\n"
	}
	return line
}

func scrubJSONStringsSlow(line string) (string, bool) {
	if !json.Valid([]byte(line)) {
		return "", false
	}
	var b strings.Builder
	last := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '"' {
			continue
		}
		j := i + 1
		for j < len(line) && line[j] != '"' {
			if line[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(line) {
			return "", false
		}
		tok := line[i : j+1]
		var text string
		if json.Unmarshal([]byte(tok), &text) == nil {
			if privateKeyOpen.MatchString(privateKey.ReplaceAllString(text, "")) {
				return "", false
			}
			if clean := Scrub(text); clean != text {
				b.WriteString(line[last:i])
				b.WriteString(jsonString(clean))
				last = j + 1
			}
		}
		i = j
	}
	b.WriteString(line[last:])
	out := b.String()
	if !json.Valid([]byte(out)) || containsSecretSlow(out) {
		return "", false
	}
	return out, true
}

func checkSame(t testing.TB, s string) {
	t.Helper()
	if got, want := ContainsSecret(s), containsSecretSlow(s); got != want {
		t.Fatalf("ContainsSecret(%q) = %v, regex-only %v", s, got, want)
	}
	if got, want := Line(s), lineSlow(s); got != want {
		t.Fatalf("Line(%q) = %q, regex-only %q", s, got, want)
	}
}

func TestSecretHintsCoverEveryPattern(t *testing.T) {
	if len(secretHints) != len(secrets) {
		t.Fatalf("%d hints for %d patterns", len(secretHints), len(secrets))
	}
}

// Every string literal in this package's tests, alone, as a JSON value,
// and padded, through both paths.
func TestPrefilterMatchesRegexOnTestLiterals(t *testing.T) {
	lits := testLiterals(t)
	if len(lits) < 50 {
		t.Fatalf("only %d literals found", len(lits))
	}
	for _, s := range lits {
		js, _ := json.Marshal(map[string]string{"text": s})
		for _, v := range []string{s, string(js), "x " + s + " y", s + strings.Repeat("a", 30)} {
			checkSame(t, v)
		}
	}
}

func testLiterals(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob("*_test.go")
	fset := token.NewFileSet()
	var out []string
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if s, err := strconv.Unquote(bl.Value); err == nil {
					out = append(out, s)
				}
			}
			return true
		})
	}
	return out
}

// pieces are the literals the patterns key on, their near misses, and the
// separators and values around them.
var pieces = []string{
	"AKIA", "ASIA", "AKIAIOSFODNN7EXAMPLE", "akia", "-----BEGIN ", "-----BEGIN RSA PRIVATE KEY-----",
	"-----END RSA PRIVATE KEY-----", "PRIVATE KEY-----", "Bearer ", "bearer ", "BEARER ", "Bearer",
	"Authorization:", "authorization: ", "AUTHORIZATION:", "Basic ", "basic\t", "gho_", "ghp_", "ghs_", "ghu_",
	"gh_", "github_pat_", "glpat-", "sk-", "sk-ant-", "sk-proj-", "SK-", "sk_live_", "rk_test_", "sk_test_",
	"k_live", "xai-", "AIza", "aiza", "xoxb-", "xoxp-", "xox", "npm_", "SG.", "sg.",
	"https://hooks.slack.com/services/T", "/B", "https://discord.com/api/webhooks/",
	"https://discordapp.com/api/webhooks/", "123456789", "eyJ", "eyj", ".",
	"postgres://", "postgresql://", "mysql://", "mongodb+srv://", "redis://", "AMQPS://", "user:", "@", "hunter2",
	"${PGPASSWORD}", "<pw>", "****", "[redacted]", "://",
	"password", "PASSWORD", "Password", "passwd", "pwd", "PWD", "secret", "SECRET", "api_key", "api-key", "apikey",
	"API_KEY", "access_key", "accesskey", "auth_token", "authtoken", "token", "TOKEN", "input_tokens", "tokens",
	"paſſword", "ſecret", "toKen", "KEY", "api_Key", "aws_secret_access_key", "from_secret: ",
	"=", ":", " = ", " : ", "\t:", "\n=", "\f:", "\r=", "\v=", "\"", "'", "\": \"", "/", " ", "\n", "-", "_",
	"process.env.", "base64.", "cookieStore.refreshToken", "/etc/prometheus/x", "refs/for/main", "next/font",
	"é", "\xff", "\xe2\x84", "\xc5", "K", "ſ",
}

func randValue(r *rand.Rand) string {
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const extra = "+/=_-.KMſé"
	n := r.IntN(40)
	var b strings.Builder
	for i := 0; i < n; i++ {
		if r.IntN(8) == 0 {
			rs := []rune(extra)
			b.WriteRune(rs[r.IntN(len(rs))])
		} else {
			b.WriteByte(alnum[r.IntN(len(alnum))])
		}
	}
	return b.String()
}

// mutateCase flips ASCII letters and swaps s/k for their Unicode folds.
func mutateCase(r *rand.Rand, s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case (c == 's' || c == 'S') && r.IntN(6) == 0:
			b.WriteRune('ſ')
		case (c == 'k' || c == 'K') && r.IntN(6) == 0:
			b.WriteRune('K')
		case c >= 'a' && c <= 'z' && r.IntN(3) == 0:
			b.WriteRune(c - 32)
		case c >= 'A' && c <= 'Z' && r.IntN(3) == 0:
			b.WriteRune(c + 32)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// structured builds whole tokens for the patterns with several parts,
// which random pieces almost never line up into.
func structured(r *rand.Rand) string {
	v := func() string {
		if r.IntN(2) == 0 {
			return randValue(r) + strings.Repeat("a", r.IntN(24))
		}
		return strings.Repeat("Zx9", 1+r.IntN(10))
	}
	digits := strconv.Itoa(r.IntN(1 << 30))
	switch r.IntN(7) {
	case 6:
		scheme := []string{"postgres", "postgresql", "mysql", "mongodb+srv", "redis", "rediss", "amqps", "mssql"}[r.IntN(8)]
		pw := []string{v(), "${PGPASSWORD}", "<pw>", "****", "hunter2"}[r.IntN(5)]
		return scheme + "://user" + digits + ":" + pw + "@db.internal/app"
	case 0:
		return "SG." + v() + "." + v()
	case 1:
		return "https://hooks.slack.com/services/T" + v() + "/B" + v() + "/" + v()
	case 2:
		host := []string{"discord.com", "discordapp.com", "Discord.com"}[r.IntN(3)]
		return "https://" + host + "/api/webhooks/" + digits + "/" + v()
	case 3:
		return "eyJ" + v() + "." + v() + "." + v()
	case 4:
		return []string{"Authorization:", "authorization: ", "AUTHORIZATION:\t"}[r.IntN(3)] + []string{"Basic ", "basic  ", "BASIC\t"}[r.IntN(3)] + v()
	default:
		return []string{"Bearer ", "bearer ", "BEARER "}[r.IntN(3)] + v()
	}
}

func genCase(r *rand.Rand) string {
	var b strings.Builder
	for n := 1 + r.IntN(8); n > 0; n-- {
		switch r.IntN(5) {
		case 0:
			b.WriteString(randValue(r))
		case 4:
			if r.IntN(2) == 0 {
				b.WriteString(structured(r))
			} else {
				b.WriteString(mutateCase(r, structured(r)))
			}
		case 1:
			b.WriteString(mutateCase(r, pieces[r.IntN(len(pieces))]))
		default:
			b.WriteString(pieces[r.IntN(len(pieces))])
		}
	}
	s := b.String()
	if r.IntN(4) == 0 {
		js, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": s}, "usage": map[string]int{"input_tokens": 12}})
		s = string(js)
	}
	return s
}

func TestPrefilterMatchesRegexOnGeneratedCorpus(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 44))
	n := 30000
	if testing.Short() {
		n = 3000
	}
	hits := 0
	// Each pattern must match some case, or a wrong hint for it would
	// pass unseen.
	matched := make([]int, len(secrets)+2)
	for i := 0; i < n; i++ {
		s := genCase(r)
		checkSame(t, s)
		if containsSecretSlow(s) {
			hits++
		}
		for j, re := range secrets {
			if re.MatchString(s) {
				matched[j]++
			}
		}
		for _, m := range dsnPassword.FindAllStringSubmatch(s, -1) {
			if !placeholder(m[2]) {
				matched[len(secrets)]++
			}
		}
		for _, m := range secretAssign.FindAllStringSubmatch(s, -1) {
			if assignedSecret(m[0], m[1]) {
				matched[len(secrets)+1]++
			}
		}
	}
	if hits < n/50 || hits > n-n/50 {
		t.Fatalf("%d of %d generated cases hold a secret; corpus is lopsided", hits, n)
	}
	for j, c := range matched {
		if c == 0 {
			t.Errorf("pattern %d never matched in the generated corpus", j)
		}
	}
	t.Logf("%d generated cases, %d with a secret, per-pattern matches %v", n, hits, matched)
}

// The (?i) patterns match ſ for s and the Kelvin sign for k; the
// prefilter must too.
func TestPrefilterFoldsLikeTheRegex(t *testing.T) {
	for _, s := range []string{
		"paſſword=hunter2abc1",
		"PAſſWORD = \"Zx9qLm2Ab\"",
		"my_ſecret: 12345678abcd",
		"api_\u212Aey=abcd1234efgh",
		"to\u212Aen\t:\tabcd1234efgh",
		"ACCESS-KEY=abcdefgh12345678",
		"BEARER " + strings.Repeat("Q", 24),
		"bEaReR " + strings.Repeat("ſ", 24),
		"AUTHORIZATION:  basic " + strings.Repeat("A", 16),
		"POSTGRES://u:hunter2@db",
	} {
		if !containsSecretSlow(s) {
			t.Fatalf("regex-only path misses %q; the case proves nothing", s)
		}
		checkSame(t, s)
	}
}

func FuzzLineMatchesSlow(f *testing.F) {
	r := rand.New(rand.NewPCG(2, 44))
	for _, p := range pieces {
		f.Add(p)
	}
	for i := 0; i < 200; i++ {
		f.Add(genCase(r))
	}
	f.Fuzz(func(t *testing.T, s string) {
		checkSame(t, s)
	})
}

// TestPrefilterMatchesRegexOnRealCorpus compares the two paths on every
// line of the .jsonl files under LOSSLESS_REDACT_CORPUS (read only), up to
// LOSSLESS_REDACT_CORPUS_MB megabytes (default 1024) in a fixed shuffle.
//
//	LOSSLESS_REDACT_CORPUS=$HOME/.claude/projects go test ./internal/redact -run RealCorpus -v -timeout 30m
func TestPrefilterMatchesRegexOnRealCorpus(t *testing.T) {
	root := os.Getenv("LOSSLESS_REDACT_CORPUS")
	if root == "" {
		t.Skip("set LOSSLESS_REDACT_CORPUS to a directory of .jsonl transcripts")
	}
	maxMB, _ := strconv.Atoi(os.Getenv("LOSSLESS_REDACT_CORPUS_MB"))
	if maxMB <= 0 {
		maxMB = 1024
	}
	var files []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	rand.New(rand.NewPCG(3, 44)).Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })

	var budget atomic.Int64
	budget.Store(int64(maxMB) << 20)
	var lines, bytes, secretLines, mismatches, nFiles atomic.Int64
	var fastNS, slowNS atomic.Int64
	var mu sync.Mutex
	var firstBad string
	work := make(chan string)
	var wg sync.WaitGroup
	workers := max(1, runtime.NumCPU()/2)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range work {
				f, err := os.Open(p)
				if err != nil {
					continue
				}
				nFiles.Add(1)
				br := bufio.NewReaderSize(f, 1<<20)
				for {
					line, err := br.ReadString('\n')
					if line != "" {
						lines.Add(1)
						bytes.Add(int64(len(line)))
						t0 := time.Now()
						got := Line(line)
						t1 := time.Now()
						want := lineSlow(line)
						t2 := time.Now()
						fastNS.Add(int64(t1.Sub(t0)))
						slowNS.Add(int64(t2.Sub(t1)))
						if want != line && want != line+"\n" {
							secretLines.Add(1)
						}
						if got != want || ContainsSecret(line) != containsSecretSlow(line) {
							mismatches.Add(1)
							mu.Lock()
							if firstBad == "" {
								firstBad = p
							}
							mu.Unlock()
						}
					}
					if err == io.EOF || err != nil {
						break
					}
				}
				_ = f.Close()
			}
		}()
	}
	for _, p := range files {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if budget.Add(-fi.Size()) < 0 {
			break
		}
		work <- p
	}
	close(work)
	wg.Wait()
	mb := float64(bytes.Load()) / (1 << 20)
	t.Logf("%d files, %d lines, %.0f MB, %d lines redacted, %d mismatches", nFiles.Load(), lines.Load(), mb, secretLines.Load(), mismatches.Load())
	if f, s := time.Duration(fastNS.Load()).Seconds(), time.Duration(slowNS.Load()).Seconds(); f > 0 {
		t.Logf("Line: %.1f MB/s with prefilter, %.1f MB/s regex-only (%.1fx)", mb/f, mb/s, s/f)
	}
	if mismatches.Load() > 0 {
		t.Fatalf("%d mismatches, first in %s", mismatches.Load(), firstBad)
	}
}

// benchLines is a transcript-like mix: assistant prose, tool calls and
// results, usage counters, and an occasional secret.
func benchLines() []string {
	r := rand.New(rand.NewPCG(4, 44))
	var out []string
	prose := "The watcher stats each transcript once per tick; a cold file is checked on its slot. Run go test ./internal/watch and check the tick cost."
	for i := 0; i < 200; i++ {
		var v any
		switch i % 5 {
		case 0:
			v = map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": prose}}, "usage": map[string]int{"input_tokens": 1200 + i, "output_tokens": 300, "cache_read_input_tokens": 90000}}}
		case 1:
			v = map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]string{"command": "cd /Users/me/dev/app && go test ./... 2>&1 | tail -20", "description": "Run tests"}}}}}
		case 2:
			v = map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": strings.Repeat("ok  \tlossless/internal/watch\t3.677s\n", 20) + randValue(r)}}}}
		case 3:
			v = map[string]any{"type": "user", "message": map[string]any{"content": "please make the token refresh use the new key: config.auth.key, not the old one"}, "cwd": "/Users/me/dev/app", "sessionId": "5d0c3d1e-0d6b-4f0e-9a7e-2b1f0c9b8a77"}
		default:
			s := prose
			if i%25 == 4 {
				s += " export API_KEY=" + strings.Repeat("a1", 16)
			}
			v = map[string]any{"type": "assistant", "message": map[string]any{"content": s}}
		}
		b, _ := json.Marshal(v)
		out = append(out, string(b)+"\n")
	}
	return out
}

func benchLine(b *testing.B, fn func(string) string) {
	lines := benchLines()
	n := 0
	for _, l := range lines {
		n += len(l)
	}
	b.SetBytes(int64(n / len(lines)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fn(lines[i%len(lines)])
	}
}

func BenchmarkLine(b *testing.B) {
	benchLine(b, Line)
}

func BenchmarkLineSlow(b *testing.B) {
	benchLine(b, lineSlow)
}
