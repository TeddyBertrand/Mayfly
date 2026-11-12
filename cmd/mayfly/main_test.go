package main

import (
	"bytes"
	"testing"
)

// TestPrintVersion checks the version flags and that other arguments are ignored.
func TestPrintVersion(t *testing.T) {
	tests := []struct {
		args    []string
		printed bool
	}{
		{[]string{"--version"}, true},
		{[]string{"-version"}, true},
		{nil, false},
		{[]string{"--version", "extra"}, false},
		{[]string{"serve"}, false},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		if got := printVersion(tt.args, &out); got != tt.printed {
			t.Errorf("printVersion(%v) = %v, want %v", tt.args, got, tt.printed)
		}
		if tt.printed && out.String() != version+"\n" {
			t.Errorf("printVersion(%v) wrote %q", tt.args, out.String())
		}
	}
}
