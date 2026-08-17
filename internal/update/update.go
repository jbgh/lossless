package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const DefaultRepo = "jbgh/lossless"

const DefaultAPI = "https://api.github.com"

const checksumName = "SHA256SUMS"

const maxAssetBytes = 80 << 20

type Options struct {
	Repo      string
	API       string
	Tag       string // empty = latest
	Dest      string
	UserHome  string
	Current   string
	GOOS      string
	GOARCH    string
	CheckOnly bool
	HTTP      *http.Client
	Token     string
}

type Result struct {
	Version         string
	Tag             string
	Dest            string
	AlreadyLatest   bool
	UpdateAvailable bool
	Replaced        bool
	ChannelInstall  bool // dest was a symlink or missing; now a release binary
}

type Release struct {
	Tag     string
	Version string
	Assets  []Asset
}

type Asset struct {
	Name string
	URL  string
	Size int64
}

func DefaultDest(userHome string) string {
	if userHome == "" {
		userHome, _ = os.UserHomeDir()
	}
	if userHome == "" {
		userHome = os.Getenv("HOME")
	}
	return filepath.Join(userHome, ".local", "bin", "lossless")
}

func AssetName(goos, goarch string) string {
	return "lossless-" + goos + "-" + goarch
}

func NormalizeTag(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(s, "v")
}

func Compare(a, b string) int {
	as := parseSemver(NormalizeTag(a))
	bs := parseSemver(NormalizeTag(b))
	for i := 0; i < 3; i++ {
		if as[i] < bs[i] {
			return -1
		}
		if as[i] > bs[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(s string) [3]int {
	var out [3]int
	s = strings.SplitN(s, "-", 2)[0]
	s = strings.SplitN(s, "+", 2)[0]
	parts := strings.Split(s, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n := 0
		for _, r := range parts[i] {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}

func LooksLikeSourceTree(exe string) bool {
	if exe == "" {
		return false
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "lossless")); err != nil {
		return false
	}
	return true
}

func DestOffChannel(dest string) bool {
	if dest == "" {
		return true
	}
	fi, err := os.Lstat(dest)
	if err != nil {
		return true
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if !fi.Mode().IsRegular() {
		return true
	}
	return false
}

func Apply(opts Options) (Result, error) {
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.API == "" {
		opts.API = DefaultAPI
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.Dest == "" {
		opts.Dest = DefaultDest(opts.UserHome)
	}
	if opts.HTTP == nil {
		opts.HTTP = Client(opts.API)
	}

	rel, err := FetchRelease(opts)
	if err != nil {
		return Result{}, err
	}
	out := Result{Version: rel.Version, Tag: rel.Tag, Dest: opts.Dest}
	off := DestOffChannel(opts.Dest)
	same := opts.Current != "" && Compare(opts.Current, rel.Version) == 0
	newer := opts.Current == "" || Compare(opts.Current, rel.Version) < 0
	out.UpdateAvailable = newer || off
	out.ChannelInstall = off
	if same && !off {
		out.AlreadyLatest = true
		out.UpdateAvailable = false
		return out, nil
	}
	if opts.CheckOnly {
		return out, nil
	}

	asset, ok := rel.Lookup(AssetName(opts.GOOS, opts.GOARCH))
	if !ok {
		return out, fmt.Errorf("release %s has no %s binary", rel.Tag, AssetName(opts.GOOS, opts.GOARCH))
	}
	sums, err := download(opts, rel, checksumName)
	if err != nil {
		return out, fmt.Errorf("checksums: %w", err)
	}
	want, err := checksumFor(sums, asset.Name)
	if err != nil {
		return out, err
	}
	body, err := download(opts, rel, asset.Name)
	if err != nil {
		return out, err
	}
	if err := verifyBinary(opts.GOOS, body, want); err != nil {
		return out, err
	}
	if err := installBinary(opts.Dest, body); err != nil {
		return out, err
	}
	out.Replaced = true
	out.ChannelInstall = false
	return out, nil
}

func (r Release) Lookup(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

func FetchRelease(opts Options) (Release, error) {
	if opts.Repo == "" || strings.Contains(opts.Repo, "://") || strings.ContainsAny(opts.Repo, " \t\n") {
		return Release{}, fmt.Errorf("invalid repo")
	}
	parts := strings.Split(opts.Repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Release{}, fmt.Errorf("invalid repo %q (want owner/name)", opts.Repo)
	}
	path := "/repos/" + opts.Repo + "/releases/latest"
	if tag := strings.TrimSpace(opts.Tag); tag != "" {
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		path = "/repos/" + opts.Repo + "/releases/tags/" + url.PathEscape(tag)
	}
	raw, err := get(opts, strings.TrimRight(opts.API, "/")+path, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	if body.TagName == "" {
		return Release{}, fmt.Errorf("release has no tag")
	}
	rel := Release{Tag: body.TagName, Version: NormalizeTag(body.TagName)}
	for _, a := range body.Assets {
		if a.Name == "" || a.BrowserDownloadURL == "" {
			continue
		}
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
	}
	if len(rel.Assets) == 0 {
		return Release{}, fmt.Errorf("release %s has no assets", rel.Tag)
	}
	return rel, nil
}

func download(opts Options, rel Release, name string) ([]byte, error) {
	if a, ok := rel.Lookup(name); ok {
		return get(opts, a.URL, "application/octet-stream")
	}
	base := strings.TrimRight(opts.API, "/")
	// Fallback for tests that only stub /download/<name> under the API host.
	return get(opts, base+"/download/"+url.PathEscape(name), "application/octet-stream")
}

func checksumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		file := strings.TrimPrefix(fields[1], "*")
		if file == name {
			sum := strings.ToLower(fields[0])
			if len(sum) != 64 {
				return "", fmt.Errorf("bad checksum for %s", name)
			}
			for _, r := range sum {
				if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
					return "", fmt.Errorf("bad checksum for %s", name)
				}
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("%s missing from %s", name, checksumName)
}

func verifyBinary(goos string, body []byte, wantHex string) error {
	if len(body) < 4 {
		return fmt.Errorf("downloaded asset is too small")
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if got != wantHex {
		return fmt.Errorf("checksum mismatch (got %s)", got)
	}
	if !looksLikeBinary(goos, body) {
		return fmt.Errorf("downloaded asset is not a %s binary", goos)
	}
	return nil
}

func looksLikeBinary(goos string, b []byte) bool {
	if len(b) < 4 {
		return false
	}
	switch goos {
	case "linux":
		return b[0] == 0x7f && b[1] == 'E' && b[2] == 'L' && b[3] == 'F'
	case "darwin":
		be := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		switch be {
		case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, 0xcafebabe, 0xcafebabf:
			return true
		}
		return false
	default:
		return true
	}
}

func installBinary(dest string, body []byte) error {
	if dest == "" || dest == "." || dest == "/" {
		return fmt.Errorf("invalid install path")
	}
	if fi, err := os.Lstat(dest); err == nil {
		if fi.IsDir() {
			return fmt.Errorf("refusing to overwrite directory %s", dest)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func Client(api string) *http.Client {
	return &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("too many redirects")
			}
			if err := checkURL(req.URL, api); err != nil {
				return err
			}
			return nil
		},
	}
}

func get(opts Options, rawURL, accept string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := checkURL(u, opts.API); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "lossless")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if tok := strings.TrimSpace(opts.Token); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := opts.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("%s: %s", u.Redacted(), res.Status)
	}
	if res.ContentLength > maxAssetBytes {
		return nil, fmt.Errorf("asset too large")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxAssetBytes {
		return nil, fmt.Errorf("asset too large")
	}
	return body, nil
}

func checkURL(u *url.URL, api string) error {
	if u == nil || u.Host == "" {
		return fmt.Errorf("invalid URL")
	}
	host := strings.ToLower(u.Hostname())
	apiHost := ""
	if p, err := url.Parse(api); err == nil {
		apiHost = strings.ToLower(p.Hostname())
	}
	if !allowedHost(host, apiHost) {
		return fmt.Errorf("refusing host %s", host)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf("refusing non-https %s", u.Redacted())
}

func allowedHost(host, apiHost string) bool {
	if host == "" {
		return false
	}
	if apiHost != "" && host == apiHost {
		return true
	}
	switch host {
	case "api.github.com", "github.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
		"github-releases.githubusercontent.com":
		return true
	}
	return isLoopbackHost(host) && isLoopbackHost(apiHost)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func TokenFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}
