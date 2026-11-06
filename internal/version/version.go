// Package version holds build version information for the backend binary.
package version

import (
	"runtime/debug"
	"strings"
)

const unknown = "unknown"

// Info is the resolved version information.
type Info struct {
	Version    string
	Commit     string
	IsDevBuild bool
}

var current = Info{Version: unknown, Commit: unknown, IsDevBuild: true}

// Init resolves and records the process-wide version info from the link-time values
// (set via -ldflags "-X main.version=... -X main.commit=...") falling back to the Go
// build info. It is meant to be called once at startup.
func Init(ldflagsVersion, ldflagsCommit string) Info {
	current = Resolve(ldflagsVersion, ldflagsCommit, readBuildInfo())
	return current
}

// Current returns the info recorded by Init.
func Current() Info { return current }

func readBuildInfo() *debug.BuildInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return bi
}

// Resolve computes Info from ldflags-provided values and (optionally) build info.
// ldflags values take precedence; a release build is one where ldflagsVersion is set.
func Resolve(ldflagsVersion, ldflagsCommit string, bi *debug.BuildInfo) Info {
	if ldflagsVersion != "" && ldflagsVersion != unknown {
		c := ldflagsCommit
		if c == "" {
			c = unknown
		}
		return Info{Version: ldflagsVersion, Commit: c, IsDevBuild: false}
	}

	var revision string
	var modified bool
	var modVersion string
	if bi != nil {
		modVersion = bi.Main.Version
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}

	commit := ldflagsCommit
	if commit == "" {
		commit = unknown
	}
	short := revision
	if len(short) > 7 {
		short = short[:7]
	}
	if commit == unknown && revision != "" {
		commit = revision
	}

	// Main module version is a real tag only for `go install pkg@vX.Y.Z` builds.
	// Local builds report "(devel)" or a pseudo-version, which we ignore.
	base := ""
	if isTagVersion(modVersion) {
		base = modVersion
	}

	v := "dev"
	if base != "" {
		v = base + "-dev"
	}
	if short != "" {
		v += "+" + short
	}
	if modified {
		v += "-dirty"
	}
	return Info{Version: v, Commit: commit, IsDevBuild: true}
}

// isTagVersion reports whether v looks like a clean semver tag (no pseudo-version or +dirty suffix).
func isTagVersion(v string) bool {
	if !strings.HasPrefix(v, "v") || strings.Contains(v, "+") {
		return false
	}
	// pseudo-versions look like v1.2.3-0.20240101000000-abcdef123456
	if strings.Count(v, "-") > 0 {
		parts := strings.Split(v, "-")
		last := parts[len(parts)-1]
		if len(last) == 12 {
			return false
		}
	}
	return true
}
