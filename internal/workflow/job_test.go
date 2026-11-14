package workflow

import "testing"

// TestMatchesLabels checks that every required label must be present, case-insensitively.
func TestMatchesLabels(t *testing.T) {
	tests := []struct {
		name     string
		labels   []string
		required []string
		want     bool
	}{
		{"exact", []string{"mayfly"}, []string{"mayfly"}, true},
		{"with self-hosted", []string{"self-hosted", "mayfly"}, []string{"mayfly"}, true},
		{"case-insensitive", []string{"Mayfly"}, []string{"mayfly"}, true},
		{"github-hosted", []string{"ubuntu-latest"}, []string{"mayfly"}, false},
		{"missing one of two", []string{"mayfly"}, []string{"mayfly", "linux"}, false},
		{"no labels", nil, []string{"mayfly"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesLabels(tt.labels, tt.required); got != tt.want {
				t.Errorf("MatchesLabels = %v, want %v", got, tt.want)
			}
		})
	}
}
