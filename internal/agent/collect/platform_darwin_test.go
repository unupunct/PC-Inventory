//go:build darwin

package collect

import "testing"

func TestParseMacBytes(t *testing.T) {
	cases := map[string]uint64{"8 GB": 8 << 30, "16 GiB": 16 << 30, "512 MB": 512 << 20, "": 0, "nonsense": 0}
	for in, want := range cases {
		if got := parseMacBytes(in); got != want {
			t.Errorf("parseMacBytes(%q) = %d, want %d", in, got, want)
		}
	}
}
