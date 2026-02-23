package workspace

import (
	"testing"
	"time"
)

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name       string
		lastCommit time.Time
		want       string
	}{
		{"zero", time.Time{}, ""},
		{"today", time.Now(), "today"},
		{"1 day", time.Now().Add(-36 * time.Hour), "1 day ago"},
		{"5 days", time.Now().Add(-5 * 24 * time.Hour), "5 days ago"},
		{"45 days", time.Now().Add(-45 * 24 * time.Hour), "1 month ago"},
		{"90 days", time.Now().Add(-90 * 24 * time.Hour), "3 months ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatAge(tt.lastCommit); got != tt.want {
				t.Errorf("FormatAge() = %q, want %q", got, tt.want)
			}
		})
	}
}
