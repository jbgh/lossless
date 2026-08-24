package version

// Version is the release semver. Release builds override it with
// -X lossless/internal/version.Version=<tag>.
var Version = "0.1.16"

// Commit is the short git SHA when set at link time. Empty for a plain go build.
var Commit = ""

func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
