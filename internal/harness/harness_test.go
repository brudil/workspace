package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func fakeCursor(found map[string]string, chatOut string, chatErr error) *Cursor {
	return &Cursor{
		lookPath: func(name string) (string, error) {
			if p, ok := found[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		run: func(ctx context.Context, bin string, args ...string) ([]byte, error) {
			return []byte(chatOut), chatErr
		},
	}
}

func TestCursor_BinaryLookup(t *testing.T) {
	t.Setenv(EnvCursorBin, "")
	if err := fakeCursor(nil, "", nil).Available(); err == nil || !strings.Contains(err.Error(), EnvCursorBin) {
		t.Errorf("err = %v", err)
	}
	c := fakeCursor(map[string]string{"agent": "/bin/agent"}, "", nil)
	if b, _ := c.bin(); b != "/bin/agent" {
		t.Errorf("fallback bin = %q", b)
	}
	c = fakeCursor(map[string]string{"cursor-agent": "/bin/ca", "agent": "/bin/agent"}, "", nil)
	if b, _ := c.bin(); b != "/bin/ca" {
		t.Errorf("preferred bin = %q", b)
	}
	t.Setenv(EnvCursorBin, "/opt/my-agent")
	c = fakeCursor(map[string]string{"/opt/my-agent": "/opt/my-agent"}, "", nil)
	if b, _ := c.bin(); b != "/opt/my-agent" {
		t.Errorf("override bin = %q", b)
	}
}

func TestCursor_StartWithChatID(t *testing.T) {
	t.Setenv(EnvCursorBin, "")
	c := fakeCursor(map[string]string{"cursor-agent": "/bin/ca"}, "Created chat\n3f2a9c1e-7b4d-4e2a-9f00-1234abcd\n", nil)
	l, err := c.Start(Brief{Slug: "auth", Title: "Auth", Brief: "Fix it.", Dir: "/w/fe/auth"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Conversation != "3f2a9c1e-7b4d-4e2a-9f00-1234abcd" {
		t.Errorf("conversation = %q", l.Conversation)
	}
	if len(l.Args) != 4 || l.Args[0] != "/bin/ca" || l.Args[1] != "--resume" || l.Args[2] != l.Conversation {
		t.Errorf("args = %q", l.Args)
	}
	if !strings.Contains(l.Args[3], "ws mission show -f llm") || !strings.Contains(l.Args[3], "Fix it.") {
		t.Errorf("prompt = %q", l.Args[3])
	}
	if l.Dir != "/w/fe/auth" {
		t.Errorf("dir = %q", l.Dir)
	}
}

func TestCursor_StartWithoutChatID(t *testing.T) {
	t.Setenv(EnvCursorBin, "")
	for name, c := range map[string]*Cursor{
		"create-chat fails":     fakeCursor(map[string]string{"cursor-agent": "/bin/ca"}, "", errors.New("unknown command")),
		"create-chat is chatty": fakeCursor(map[string]string{"cursor-agent": "/bin/ca"}, "Usage: cursor-agent [options]", nil),
	} {
		l, err := c.Start(Brief{Slug: "x", Title: "X"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if l.Conversation != "" || len(l.Args) != 2 {
			t.Errorf("%s: launch = %+v", name, l)
		}
	}
}

func TestCursor_Resume(t *testing.T) {
	t.Setenv(EnvCursorBin, "")
	c := fakeCursor(map[string]string{"cursor-agent": "/bin/ca"}, "", nil)
	l, err := c.Resume("/w", "abc123")
	if err != nil || strings.Join(l.Args, " ") != "/bin/ca --resume abc123" {
		t.Errorf("resume = %+v, %v", l, err)
	}
	if _, err := c.Resume("/w", ""); err == nil {
		t.Error("resume without a chat id should fail")
	}
}

func TestShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, err := Get("shell")
	if err != nil {
		t.Fatal(err)
	}
	l, _ := h.Start(Brief{Dir: "/w"})
	if strings.Join(l.Args, " ") != "/bin/sh -i" || l.Dir != "/w" {
		t.Errorf("launch = %+v", l)
	}
	if _, err := h.Resume("/w", "x"); !errors.Is(err, ErrNoResume) {
		t.Errorf("resume err = %v", err)
	}
}

func TestRegistryAndDefault(t *testing.T) {
	if got := strings.Join(Names(), ","); got != "cursor,shell" {
		t.Errorf("names = %s", got)
	}
	if _, err := Get("nope"); err == nil {
		t.Error("unknown harness should error")
	}
	t.Setenv(EnvDefault, "")
	if Default() != "cursor" {
		t.Errorf("default = %s", Default())
	}
	t.Setenv(EnvDefault, "shell")
	if Default() != "shell" {
		t.Errorf("default = %s", Default())
	}
}
