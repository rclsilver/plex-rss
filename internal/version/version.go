package version

var (
	version = "development"
	commit  = "unknown"
)

// Version returns the version string
func Version() string {
	return version
}

// Commit returns the git commit hash
func Commit() string {
	return commit
}

// VersionFull returns the full version string with commit
func VersionFull() string {
	result := version
	if len(commit) > 0 && commit != "unknown" {
		result += " (" + commit + ")"
	}
	return result
}
