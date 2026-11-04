package cli

import (
	"bytes"
	"testing"
)

func TestZoomFilter(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
		exit   bool
	}{
		{"plain bytes pass", []string{"hello\r"}, "hello\r", false},
		{"ctrl+c passes", []string{"\x03"}, "\x03", false},
		{"ctrl+g z exits", []string{"ab\x07z"}, "ab", true},
		{"ctrl+g Z exits", []string{"\x07Z"}, "", true},
		{"ctrl+g ctrl+g sends one", []string{"\x07\x07x"}, "\x07x", false},
		{"ctrl+g other passes both", []string{"\x07a"}, "\x07a", false},
		{"split across chunks", []string{"ab\x07", "z"}, "ab", true},
		{"kitty ctrl+g z exits", []string{"\x1b[103;5uz"}, "", true},
		{"kitty split mid-sequence", []string{"x\x1b[103", ";5u", "z"}, "x", true},
		{"other escapes pass", []string{"\x1b[A\x1b[103;6u"}, "\x1b[A\x1b[103;6u", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f zoomFilter
			var got []byte
			exit := false
			for _, c := range tt.chunks {
				fwd, e := f.feed([]byte(c))
				got = append(got, fwd...)
				if e {
					exit = true
					break
				}
			}
			if !bytes.Equal(got, []byte(tt.want)) || exit != tt.exit {
				t.Errorf("got %q exit=%v, want %q exit=%v", got, exit, tt.want, tt.exit)
			}
		})
	}
}
