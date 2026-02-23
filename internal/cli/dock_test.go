package cli

import "testing"

func TestParsePRURL_Valid(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantOrg  string
		wantRepo string
		wantNum  int
	}{
		{
			name:     "simple PR URL",
			url:      "https://github.com/my-org/my-repo/pull/123",
			wantOrg:  "my-org",
			wantRepo: "my-repo",
			wantNum:  123,
		},
		{
			name:     "PR URL with trailing slash",
			url:      "https://github.com/org/repo/pull/42/",
			wantOrg:  "org",
			wantRepo: "repo",
			wantNum:  42,
		},
		{
			name:     "PR URL with extra path segments",
			url:      "https://github.com/org/repo/pull/99/files",
			wantOrg:  "org",
			wantRepo: "repo",
			wantNum:  99,
		},
		{
			name:     "HTTP URL",
			url:      "http://github.com/org/repo/pull/7",
			wantOrg:  "org",
			wantRepo: "repo",
			wantNum:  7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, repo, num, ok := parsePRURL(tt.url)
			if !ok {
				t.Fatal("parsePRURL() returned ok=false, want true")
			}
			if org != tt.wantOrg {
				t.Errorf("org = %q, want %q", org, tt.wantOrg)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if num != tt.wantNum {
				t.Errorf("number = %d, want %d", num, tt.wantNum)
			}
		})
	}
}

func TestParsePRURL_Invalid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"not a URL", "my-branch"},
		{"github but not a PR", "https://github.com/org/repo/issues/1"},
		{"missing number", "https://github.com/org/repo/pull/"},
		{"zero PR number", "https://github.com/org/repo/pull/0"},
		{"non-github URL", "https://gitlab.com/org/repo/pull/1"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, ok := parsePRURL(tt.url)
			if ok {
				t.Error("parsePRURL() returned ok=true, want false")
			}
		})
	}
}

func TestIsPRNumber_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1", 1},
		{"42", 42},
		{"9999", 9999},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			n, ok := isPRNumber(tt.input)
			if !ok {
				t.Fatal("isPRNumber() returned ok=false, want true")
			}
			if n != tt.want {
				t.Errorf("isPRNumber() = %d, want %d", n, tt.want)
			}
		})
	}
}

func TestIsPRNumber_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"branch name", "my-branch"},
		{"zero", "0"},
		{"negative", "-1"},
		{"float", "1.5"},
		{"empty", ""},
		{"letters and digits", "pr123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := isPRNumber(tt.input)
			if ok {
				t.Errorf("isPRNumber(%q) returned ok=true, want false", tt.input)
			}
		})
	}
}
