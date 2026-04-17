package misc

import (
	"os"
	"regexp"
	"strings"
)

// selfContainerIDRegex matches the 64-hex container ID embedded in the
// various /proc/self/cgroup line formats. The regex is intentionally loose
// — it matches anywhere in a line, because cgroup paths vary across
// cgroups v1 (e.g. `12:memory:/docker/<id>`) and v2 (e.g.
// `0::/system.slice/docker-<id>.scope`, or `/docker/<id>`) — and the hex
// length is the only thing that's stable across formats.
var selfContainerIDRegex = regexp.MustCompile(`([0-9a-f]{64})`)

// SelfContainerID returns the container ID of the process, when Swirl is
// running inside a Docker container. The lookup order is:
//
//  1. `SWIRL_CONTAINER_ID` env var — explicit override, set by the operator
//     in the compose file (typical value: `${HOSTNAME}` for self-matching);
//  2. parse `/proc/self/cgroup` — works on cgroups v1 and v2 on most
//     kernels;
//  3. `os.Hostname()` — Docker sets the hostname to the short (12-char)
//     container ID by default, so this is a reasonable last resort but
//     can return false positives if the operator customised the hostname.
//
// The second return value is false when every strategy failed (typical on
// Swirl running natively during development). Callers MUST treat a
// false result as "no self-protection possible" and proceed normally.
func SelfContainerID() (string, bool) {
	if v := strings.TrimSpace(os.Getenv("SWIRL_CONTAINER_ID")); v != "" {
		return v, true
	}
	if id, ok := parseCgroupForSelfID("/proc/self/cgroup"); ok {
		return id, true
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h, true
	}
	return "", false
}

// parseCgroupForSelfID is factored out so the regex logic can be unit
// tested from a fixture file without involving /proc.
func parseCgroupForSelfID(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if m := selfContainerIDRegex.FindString(line); m != "" {
			return m, true
		}
	}
	return "", false
}

// ContainerIDMatchesSelf reports whether the given Docker container ID
// (full or 12-char short form) identifies the same container Swirl is
// running in. The check is symmetric on the short/full form: Docker
// guarantees the first 12 hex chars are unique on a given host, so a
// prefix match is correct in either direction.
func ContainerIDMatchesSelf(other string) bool {
	self, ok := SelfContainerID()
	if !ok || self == "" || other == "" {
		return false
	}
	self = strings.ToLower(strings.TrimSpace(self))
	other = strings.ToLower(strings.TrimSpace(other))
	if self == other {
		return true
	}
	// Tolerate short vs full form either way.
	if len(self) > len(other) {
		return strings.HasPrefix(self, other)
	}
	return strings.HasPrefix(other, self)
}
