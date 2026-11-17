package store

import "testing"

func TestVersionFromFilename(t *testing.T) {
	cases := map[string]string{
		"pcim-agent-1.1.0-windows-amd64.exe": "1.1.0",
		"pcim-agent-1.0.0-linux-arm64":       "1.0.0",
		"pcim-agent_2.3.4_darwin_amd64":      "2.3.4",
		"pcim-agent-windows-amd64.exe":       "",
		"pcim-agent.exe":                     "",
	}
	for in, want := range cases {
		if got := versionFromFilename(in); got != want {
			t.Errorf("versionFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionIgnoredForServerBuilds(t *testing.T) {
	// The scan skips files whose name marks them as a server build; the
	// version parser itself is name-agnostic.
	if got := versionFromFilename("pcim-server-1.1.0-windows-amd64.exe"); got != "1.1.0" {
		t.Fatalf("version parse of a server build = %q", got)
	}
}
