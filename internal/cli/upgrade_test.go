package cli

import "testing"

func TestPluralize(t *testing.T) {
	tests := []struct {
		n        int
		singular string
		plural   string
		want     string
	}{
		{0, "repo", "repos", "repos"},
		{1, "repo", "repos", "repo"},
		{2, "repo", "repos", "repos"},
		{10, "item", "items", "items"},
	}

	for _, tt := range tests {
		if got := pluralize(tt.n, tt.singular, tt.plural); got != tt.want {
			t.Errorf("pluralize(%d, %q, %q) = %q, want %q", tt.n, tt.singular, tt.plural, got, tt.want)
		}
	}
}
