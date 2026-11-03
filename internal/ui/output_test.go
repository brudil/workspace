package ui

import (
	"bytes"
	"os"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func withProfile(t *testing.T, p colorprofile.Profile) {
	t.Helper()
	old := Profile
	Profile = p
	t.Cleanup(func() { Profile = old })
}

func TestWriter_NoTTYStripsStyles(t *testing.T) {
	withProfile(t, colorprofile.NoTTY)
	var buf bytes.Buffer
	Writer(&buf).Write([]byte(Red.Render("x") + Bold.Render("y")))
	if got := buf.String(); got != "xy" {
		t.Errorf("got %q, want %q", got, "xy")
	}
}

func TestWriter_ANSIDownsamples256Colour(t *testing.T) {
	withProfile(t, colorprofile.ANSI)
	var buf bytes.Buffer
	Writer(&buf).Write([]byte(Orange.Render("x"))) // colour 208
	if bytes.Contains(buf.Bytes(), []byte("38;5;")) {
		t.Errorf("256-colour sequence not downsampled: %q", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("x")) {
		t.Errorf("text missing: %q", buf.String())
	}
}

func TestStderr_FollowsSwappedStderr(t *testing.T) {
	withProfile(t, colorprofile.NoTTY)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	Stderr.Write([]byte(Green.Render("ok")))
	os.Stderr = old
	w.Close()

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if got := buf.String(); got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
}

func TestStripStyles_KeepsHyperlinks(t *testing.T) {
	link := Hyperlink("https://example.com", "pr")
	got := StripStyles(Blue.Render(link))
	if got != link {
		t.Errorf("got %q, want %q", got, link)
	}
}
