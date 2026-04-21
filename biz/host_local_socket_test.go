package biz

import "testing"

// TestIsLocalSocketEndpoint covers the tolerant-match rules used by
// EnsureLocal to decide whether an operator-registered host already
// owns the local Docker socket.
//
// Matrix:
//
//	exact                      unix:///var/run/docker.sock
//	trailing slash             unix:///var/run/docker.sock/
//	no scheme                  /var/run/docker.sock
//	surrounding whitespace     "  unix:///var/run/docker.sock  "
//
// Rejected:
//
//	tcp://... (remote daemon)
//	ssh://... (remote via SSH)
//	unix:///var/run/other.sock (different socket path)
//	empty string
func TestIsLocalSocketEndpoint(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"exact", "unix:///var/run/docker.sock", true},
		{"trailing-slash", "unix:///var/run/docker.sock/", true},
		{"no-scheme", "/var/run/docker.sock", true},
		{"surrounding-whitespace", "  unix:///var/run/docker.sock  ", true},
		{"tcp-remote", "tcp://10.0.0.2:2375", false},
		{"ssh-remote", "ssh://user@host.example.com", false},
		{"other-socket", "unix:///var/run/other.sock", false},
		{"empty", "", false},
		{"blank", "   ", false},
		{"relative-path", "var/run/docker.sock", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isLocalSocketEndpoint(tc.in)
			if got != tc.want {
				t.Fatalf("isLocalSocketEndpoint(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
