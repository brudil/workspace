package tmux

import "testing"

func TestInTmux_ReturnsTrue_WhenEnvSet(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	if !InTmux() {
		t.Error("InTmux() = false, want true")
	}
}

func TestInTmux_ReturnsFalse_WhenEnvEmpty(t *testing.T) {
	t.Setenv("TMUX", "")
	if InTmux() {
		t.Error("InTmux() = true, want false")
	}
}

func TestParseListWindows(t *testing.T) {
	output := "Frontend:auth-flow @1\nAPI:fix-cache @2\nmc @3\n"
	got := parseListWindows(output)

	if got["Frontend:auth-flow"] != "@1" {
		t.Errorf("Frontend:auth-flow = %q, want @1", got["Frontend:auth-flow"])
	}
	if got["API:fix-cache"] != "@2" {
		t.Errorf("API:fix-cache = %q, want @2", got["API:fix-cache"])
	}
	if got["mc"] != "@3" {
		t.Errorf("mc = %q, want @3", got["mc"])
	}
}

func TestParseListWindows_EmptyOutput(t *testing.T) {
	got := parseListWindows("")
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseListPanes(t *testing.T) {
	output := "%0 zsh\n%1 vim\n%2 bash\n"
	got := parseListPanes(output)

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ID != "%0" || got[0].Command != "zsh" {
		t.Errorf("pane 0 = %+v, want %%0/zsh", got[0])
	}
	if got[1].ID != "%1" || got[1].Command != "vim" {
		t.Errorf("pane 1 = %+v, want %%1/vim", got[1])
	}
	if got[2].ID != "%2" || got[2].Command != "bash" {
		t.Errorf("pane 2 = %+v, want %%2/bash", got[2])
	}
}

func TestParseListPanes_Empty(t *testing.T) {
	got := parseListPanes("")
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestFindIdlePane_FindsShell(t *testing.T) {
	panes := []Pane{
		{ID: "%0", Command: "vim"},
		{ID: "%1", Command: "zsh"},
		{ID: "%2", Command: "node"},
	}
	id, ok := FindIdlePane(panes)
	if !ok {
		t.Fatal("expected to find idle pane")
	}
	if id != "%1" {
		t.Errorf("id = %q, want %%1", id)
	}
}

func TestFindIdlePane_AllBusy(t *testing.T) {
	panes := []Pane{
		{ID: "%0", Command: "vim"},
		{ID: "%1", Command: "node"},
	}
	_, ok := FindIdlePane(panes)
	if ok {
		t.Error("expected no idle pane")
	}
}

func TestFindIdlePane_RecognizesShells(t *testing.T) {
	shells := []string{"zsh", "bash", "fish", "sh", "dash", "ksh"}
	for _, sh := range shells {
		panes := []Pane{{ID: "%0", Command: sh}}
		id, ok := FindIdlePane(panes)
		if !ok || id != "%0" {
			t.Errorf("shell %q not recognized as idle", sh)
		}
	}
}

func TestWindowName(t *testing.T) {
	tests := []struct {
		display, capsule, want string
	}{
		{"Frontend", "auth-flow", "Frontend:auth-flow"},
		{"repo1", ".ground", "repo1:.ground"},
		{"API Server", "fix", "API Server:fix"},
	}
	for _, tt := range tests {
		got := WindowName(tt.display, tt.capsule)
		if got != tt.want {
			t.Errorf("WindowName(%q, %q) = %q, want %q", tt.display, tt.capsule, got, tt.want)
		}
	}
}
