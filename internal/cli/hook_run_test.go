package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestLineWriter_MultiLine(t *testing.T) {
	ch := make(chan string, 16)
	w := newLineWriter(ch)

	w.Write([]byte("line1\nline2\nline3\n"))
	w.Close()

	var got []string
	for line := range ch {
		got = append(got, line)
	}

	want := []string{"line1", "line2", "line3"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLineWriter_PartialLine(t *testing.T) {
	ch := make(chan string, 16)
	w := newLineWriter(ch)

	w.Write([]byte("part"))
	w.Write([]byte("ial\nfull\n"))
	w.Write([]byte("trailing"))
	w.Close()

	var got []string
	for line := range ch {
		got = append(got, line)
	}

	want := []string{"partial", "full", "trailing"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func baseHookModel() hookModel {
	return hookModel{
		lineCh: make(chan string),
		doneCh: make(chan error),
	}
}

func TestHookModel_LineMsg_AddsToBuffer(t *testing.T) {
	m := baseHookModel()

	hm, _ := m.update(hookLineMsg("hello"))

	if len(hm.lines) != 1 || hm.lines[0] != "hello" {
		t.Errorf("lines = %v, want [hello]", hm.lines)
	}
}

func TestHookModel_LineMsg_CapsAt6(t *testing.T) {
	m := baseHookModel()
	m.lines = []string{"a", "b", "c", "d", "e", "f"}

	hm, _ := m.update(hookLineMsg("g"))

	if len(hm.lines) != 6 {
		t.Fatalf("len = %d, want 6", len(hm.lines))
	}
	want := []string{"b", "c", "d", "e", "f", "g"}
	for i := range want {
		if hm.lines[i] != want[i] {
			t.Errorf("lines[%d] = %q, want %q", i, hm.lines[i], want[i])
		}
	}
}

func TestHookModel_DoneMsg_Success(t *testing.T) {
	m := baseHookModel()

	hm, cmd := m.update(hookDoneMsg{err: nil})

	if !hm.done {
		t.Error("done should be true")
	}
	if hm.err != nil {
		t.Errorf("err = %v, want nil", hm.err)
	}
	if cmd != nil {
		t.Error("expected nil cmd (wrapper handles quit)")
	}
}

func TestHookModel_DoneMsg_Error(t *testing.T) {
	m := baseHookModel()

	hm, cmd := m.update(hookDoneMsg{err: fmt.Errorf("hook failed")})

	if !hm.done {
		t.Error("done should be true")
	}
	if hm.err == nil {
		t.Error("err should be set")
	}
	if cmd != nil {
		t.Error("expected nil cmd (wrapper handles quit)")
	}
}

func TestHookModel_View_Running(t *testing.T) {
	m := baseHookModel()
	m.lines = []string{"installing deps", "compiling"}

	v := m.view("⠋")

	if !strings.Contains(v, "Running hooks") {
		t.Error("should contain 'Running hooks'")
	}
	if !strings.Contains(v, "│") {
		t.Error("should contain pipe prefix for output lines")
	}
	if strings.Contains(v, "✓") || strings.Contains(v, "✗") {
		t.Error("running state should not show ✓ or ✗")
	}
}

func TestHookModel_View_Success(t *testing.T) {
	m := baseHookModel()
	m.done = true
	m.lines = []string{"some output"}

	v := m.view("⠋")

	if !strings.Contains(v, "✓") {
		t.Error("success should show ✓")
	}
	if strings.Contains(v, "│") {
		t.Error("success should not show output lines")
	}
}

func TestHookModel_View_Error(t *testing.T) {
	m := baseHookModel()
	m.done = true
	m.err = fmt.Errorf("failed")
	m.lines = []string{"error output"}

	v := m.view("⠋")

	if !strings.Contains(v, "✗") {
		t.Error("error should show ✗")
	}
	if !strings.Contains(v, "│") {
		t.Error("error should keep output lines")
	}
}

func TestCapsuleCreateModel_ViewShowsPendingHook(t *testing.T) {
	s := spinner.New()
	s.Spinner = spinner.Dot
	op := operationModel{
		repos: []repoEntry{
			{name: "step1", displayName: "step1", state: repoRunning},
		},
		op:      func(name string) (bool, error) { return false, nil },
		spinner: s,
	}

	m := newCapsuleCreateModel(op, true, func() string { return "/tmp" }, "echo hi")
	v := m.View()

	if !strings.Contains(v, "Running hooks") {
		t.Error("should show pending 'Running hooks'")
	}
	if !strings.Contains(v, "·") {
		t.Error("pending hook should show dimmed dot")
	}
}

func TestCapsuleCreateModel_HookDoneQuitsProgram(t *testing.T) {
	s := spinner.New()
	s.Spinner = spinner.Dot
	op := operationModel{
		repos:   []repoEntry{},
		op:      func(name string) (bool, error) { return false, nil },
		spinner: s,
	}

	m := newCapsuleCreateModel(op, true, func() string { return "/tmp" }, "echo hi")
	m.phase = 1
	m.hook = newHookModel(make(chan string), make(chan error))

	result, cmd := m.Update(hookDoneMsg{err: nil})
	cm := result.(capsuleCreateModel)

	if !cm.hook.done {
		t.Error("hook should be done")
	}
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit")
	}
}

func TestCapsuleCreateModel_NoHookQuitsAfterOps(t *testing.T) {
	s := spinner.New()
	s.Spinner = spinner.Dot
	op := operationModel{
		repos: []repoEntry{
			{name: "step1", displayName: "step1", state: repoRunning},
		},
		op:      func(name string) (bool, error) { return false, nil },
		spinner: s,
		done:    0,
	}

	m := newCapsuleCreateModel(op, false, nil, "")

	// Simulate the single step completing — operationModel will return tea.Quit.
	result, cmd := m.Update(repoResultMsg{name: "step1"})
	_ = result.(capsuleCreateModel)

	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit when no hook and ops done")
	}
}

func TestCapsuleCreateModel_TransitionsToHookPhase(t *testing.T) {
	m := capsuleCreateModel{
		hasHook:   true,
		hookDirFn: func() string { return "/tmp" },
		hookCmd:   "echo hi",
	}

	// Send startHookPhaseMsg to trigger transition.
	result, _ := m.Update(startHookPhaseMsg{})
	cm := result.(capsuleCreateModel)

	if cm.phase != 1 {
		t.Errorf("phase = %d, want 1", cm.phase)
	}
}
