//go:build linux

package collect

import "testing"

func TestParseSizeUnit(t *testing.T) {
	if parseSizeUnit("GB") != 1<<30 || parseSizeUnit("MB") != 1<<20 || parseSizeUnit("KB") != 1024 {
		t.Fatal("size units wrong")
	}
	if parseSizeUnit("XB") != 1 {
		t.Fatal("unknown unit should be 1")
	}
}

func TestChassisTypeName(t *testing.T) {
	for in, want := range map[string]string{"3": "Desktop", "10": "Laptop", "17": "Server", "35": "Mini PC", "99": "", "": ""} {
		if got := chassisTypeName(in); got != want {
			t.Errorf("chassisTypeName(%q) = %q, want %q", in, got, want)
		}
	}
}
