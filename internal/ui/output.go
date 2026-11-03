package ui

import (
	"io"
	"os"
	"regexp"

	"github.com/charmbracelet/colorprofile"
)

// Profile is the colour profile used for all styled CLI output.
//
// It is detected from stderr, not stdout, so colours still work inside
// eval "$(...)" where the shell captures stdout but stderr is the terminal.
// Tests override it (see cli TestMain).
var Profile = colorprofile.Detect(os.Stderr, os.Environ())

// Stderr and Stdout downsample styled output to Profile before writing.
// They resolve os.Stderr / os.Stdout on every write so callers that swap
// those (the test harness does) still capture output.
var (
	Stderr io.Writer = profileWriter{func() io.Writer { return os.Stderr }}
	Stdout io.Writer = profileWriter{func() io.Writer { return os.Stdout }}
)

type profileWriter struct {
	target func() io.Writer
}

func (w profileWriter) Write(p []byte) (int, error) {
	return Writer(w.target()).Write(p)
}

// Writer wraps w so styled output written to it is downsampled to Profile.
func Writer(w io.Writer) io.Writer {
	return &colorprofile.Writer{Forward: w, Profile: Profile}
}

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// StripStyles removes SGR (colour and text attribute) sequences while keeping
// other escapes such as OSC 8 hyperlinks. Golden tests use it to compare view
// content independent of styling.
func StripStyles(s string) string {
	return sgrPattern.ReplaceAllString(s, "")
}
