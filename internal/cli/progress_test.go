package cli

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func baseOperationModel(parallel bool) operationModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return operationModel{
		repos: []repoEntry{
			{name: "repo1", displayName: "repo1", state: repoRunning},
			{name: "repo2", displayName: "repo2", state: repoPending},
			{name: "repo3", displayName: "repo3", state: repoPending},
		},
		op:       func(name string) (bool, error) { return false, nil },
		parallel: parallel,
		spinner:  s,
	}
}

func TestOperationUpdate_Success(t *testing.T) {
	m := baseOperationModel(true)

	msg := repoResultMsg{name: "repo1"}

	result, _ := m.Update(msg)
	om := result.(operationModel)

	if om.repos[0].state != repoDone {
		t.Errorf("state = %d, want repoDone (%d)", om.repos[0].state, repoDone)
	}
	if om.done != 1 {
		t.Errorf("done = %d, want 1", om.done)
	}
}

func TestOperationUpdate_Error(t *testing.T) {
	m := baseOperationModel(true)

	msg := repoResultMsg{name: "repo1", err: fmt.Errorf("clone failed")}

	result, _ := m.Update(msg)
	om := result.(operationModel)

	if om.repos[0].state != repoFailed {
		t.Errorf("state = %d, want repoFailed (%d)", om.repos[0].state, repoFailed)
	}
	if om.repos[0].err == nil {
		t.Error("err should be stored")
	}
}

func TestOperationUpdate_Skipped(t *testing.T) {
	m := baseOperationModel(true)

	msg := repoResultMsg{name: "repo1", skipped: true}

	result, _ := m.Update(msg)
	om := result.(operationModel)

	if om.repos[0].state != repoSkipped {
		t.Errorf("state = %d, want repoSkipped (%d)", om.repos[0].state, repoSkipped)
	}
}

func TestOperationUpdate_AllDone(t *testing.T) {
	m := baseOperationModel(true)
	m.done = 2 // two already done
	m.repos[0].state = repoDone
	m.repos[1].state = repoDone
	m.repos[2].state = repoRunning

	msg := repoResultMsg{name: "repo3"}

	_, cmd := m.Update(msg)
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit when all done")
	}
}

func TestOperationUpdate_Sequential_StartsNext(t *testing.T) {
	m := baseOperationModel(false)

	msg := repoResultMsg{name: "repo1"}

	result, cmd := m.Update(msg)
	om := result.(operationModel)

	if om.repos[0].state != repoDone {
		t.Errorf("repo1 state = %d, want repoDone", om.repos[0].state)
	}
	if om.repos[1].state != repoRunning {
		t.Errorf("repo2 state = %d, want repoRunning (%d)", om.repos[1].state, repoRunning)
	}
	if cmd == nil {
		t.Error("expected a cmd to run the next repo")
	}
}

func TestOperationUpdate_KeyCtrlC(t *testing.T) {
	m := baseOperationModel(true)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit on ctrl+c")
	}
}

func TestFormatDisplayName(t *testing.T) {
	tests := []struct {
		name         string
		displayNames map[string]string
		want         string
	}{
		{
			name:         "repo1",
			displayNames: map[string]string{},
			want:         "repo1",
		},
		{
			name:         "repo1",
			displayNames: map[string]string{"repo1": "My Repo"},
			want:         "My Repo (repo1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatDisplayName(tt.name, tt.displayNames)
			if got != tt.want {
				t.Errorf("formatDisplayName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
