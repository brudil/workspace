package ui

import "testing"

func TestHyperlink(t *testing.T) {
	got := Hyperlink("https://example.com", "click here")
	want := "\x1b]8;;https://example.com\x1b\\click here\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Hyperlink() = %q, want %q", got, want)
	}
}
