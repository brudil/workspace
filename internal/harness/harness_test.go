package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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
	// --trust: a new capsule's trust dialog would swallow the opening prompt.
	if strings.Join(l.Args, " ") != "/bin/ca --trust --resume "+l.Conversation {
		t.Errorf("args = %q (trusted, and the prompt must not be an argument)", l.Args)
	}
	if !strings.Contains(l.Prompt, "ws mission show -f llm") || !strings.Contains(l.Prompt, "Fix it.") {
		t.Errorf("prompt = %q", l.Prompt)
	}
	if l.Dir != "/w/fe/auth" {
		t.Errorf("dir = %q", l.Dir)
	}
	if len(l.Notes) != 1 || !strings.HasPrefix(l.Notes[0], "ran /bin/ca create-chat: chat 3f2a9c1e-7b4d-4e2a-9f00-1234abcd (") {
		t.Errorf("notes = %q", l.Notes)
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
		if l.Conversation != "" || strings.Join(l.Args, " ") != "/bin/ca --trust" || l.Prompt == "" {
			t.Errorf("%s: launch = %+v", name, l)
		}
		// Why it can't be resumed goes in the exec log.
		if len(l.Notes) != 1 || !strings.Contains(l.Notes[0], "can't be resumed") {
			t.Errorf("%s: notes = %q", name, l.Notes)
		}
	}
}

func TestCursor_Resume(t *testing.T) {
	t.Setenv(EnvCursorBin, "")
	c := fakeCursor(map[string]string{"cursor-agent": "/bin/ca"}, "", nil)
	l, err := c.Resume("/w", "abc123")
	if err != nil || strings.Join(l.Args, " ") != "/bin/ca --trust --resume abc123" {
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

func TestRegistry(t *testing.T) {
	if got := strings.Join(Names(), ","); got != "claude,cursor,pi,shell" {
		t.Errorf("names = %s", got)
	}
	if _, err := Get("nope"); err == nil {
		t.Error("unknown harness should error")
	}
}

func TestRoles_Crew(t *testing.T) {
	t.Setenv(EnvDefault, "")
	if p := (Roles{}).ForCrew(); p != (Pick{Harness: "cursor"}) {
		t.Errorf("default crew = %+v", p)
	}
	r := Roles{Crew: Pick{Harness: "claude", Model: "opus"}}
	if p := r.ForCrew(); p != (Pick{Harness: "claude", Model: "opus"}) {
		t.Errorf("configured crew = %+v", p)
	}
	// $WS_DECK_HARNESS wins, and the configured model stays with the
	// configured harness.
	t.Setenv(EnvDefault, "shell")
	if p := r.ForCrew(); p != (Pick{Harness: "shell"}) {
		t.Errorf("env crew = %+v", p)
	}
	t.Setenv(EnvDefault, "claude")
	if p := r.ForCrew(); p != (Pick{Harness: "claude", Model: "opus"}) {
		t.Errorf("env naming the configured harness keeps its model: %+v", p)
	}
}

func TestRoles_Briefing(t *testing.T) {
	t.Setenv(EnvDefault, "")
	crew := Pick{Harness: "cursor", Model: "claude-opus-5-5-high"}
	if p := (Roles{Crew: crew}).ForBriefing(); p != crew {
		t.Errorf("briefing follows crew: %+v", p)
	}
	if p := (Roles{Crew: crew, Briefing: Pick{Model: "gpt-5.5-high"}}).ForBriefing(); p != (Pick{Harness: "cursor", Model: "gpt-5.5-high"}) {
		t.Errorf("briefing model on crew's harness: %+v", p)
	}
	if p := (Roles{Crew: crew, Briefing: Pick{Harness: "claude"}}).ForBriefing(); p != (Pick{Harness: "claude"}) {
		t.Errorf("briefing harness of its own: %+v", p)
	}
}

func TestRoles_Orchestrator(t *testing.T) {
	t.Setenv(EnvDefault, "")
	pick, o, err := (Roles{}).ForOrchestrator()
	if err != nil || o.Name() != "cursor" || pick != (Pick{Harness: "cursor", Model: CursorOrchestratorModel}) {
		t.Errorf("default = %+v %v", pick, err)
	}
	// Follows crew's harness, with that harness's own model.
	pick, _, _ = (Roles{Crew: Pick{Harness: "claude", Model: "opus"}}).ForOrchestrator()
	if pick != (Pick{Harness: "claude", Model: ClaudeOrchestratorModel}) {
		t.Errorf("claude crew = %+v", pick)
	}
	// Configured outright, or just its model.
	pick, _, _ = (Roles{Crew: Pick{Harness: "claude"}, Orchestrator: Pick{Harness: "cursor", Model: "auto"}}).ForOrchestrator()
	if pick != (Pick{Harness: "cursor", Model: "auto"}) {
		t.Errorf("configured = %+v", pick)
	}
	pick, _, _ = (Roles{Orchestrator: Pick{Model: "auto"}}).ForOrchestrator()
	if pick != (Pick{Harness: "cursor", Model: "auto"}) {
		t.Errorf("model only = %+v", pick)
	}
	// The shell can't run headless: an explicit choice says so.
	if _, _, err := (Roles{Orchestrator: Pick{Harness: "shell"}}).ForOrchestrator(); err == nil || !strings.Contains(err.Error(), "can't run the headless orchestrator") {
		t.Errorf("shell orchestrator err = %v", err)
	}
}

func TestPick_ModelBelongsToItsHarness(t *testing.T) {
	p := Pick{Harness: "claude", Model: "opus"}
	if p.ModelFor("claude") != "opus" || p.ModelFor("cursor") != "" {
		t.Errorf("ModelFor: %q %q", p.ModelFor("claude"), p.ModelFor("cursor"))
	}
}

func fakeClaude(found map[string]string) *Claude {
	return &Claude{
		lookPath: func(name string) (string, error) {
			if p, ok := found[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		newID: func() (string, error) { return "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed", nil },
	}
}

func TestClaude_Start(t *testing.T) {
	t.Setenv(EnvClaudeBin, "")
	c := fakeClaude(map[string]string{"claude": "/bin/claude"})
	l, err := c.Start(Brief{Slug: "auth", Title: "Auth", Brief: "Fix it.", Dir: "/w/fe/auth", Model: "opus"})
	if err != nil {
		t.Fatal(err)
	}
	const id = "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed"
	want := []string{"/bin/claude", "--session-id", id, "--name", "ws auth", "--model", "opus", "--"}
	if len(l.Args) != len(want)+1 || strings.Join(l.Args[:len(want)], " ") != strings.Join(want, " ") {
		t.Fatalf("args = %q", l.Args)
	}
	// The prompt is the argument, not typed in: typed, its Enter would
	// answer a trust dialog's default "No, exit".
	if prompt := l.Args[len(want)]; !strings.Contains(prompt, "ws mission show -f llm") || !strings.Contains(prompt, "Fix it.") || l.Prompt != "" {
		t.Errorf("prompt arg = %q, typed = %q", prompt, l.Prompt)
	}
	if l.Conversation != id || l.Dir != "/w/fe/auth" {
		t.Errorf("launch = %+v", l)
	}
	if len(l.Notes) != 2 || !strings.Contains(l.Notes[1], "opening prompt is its last argument") {
		t.Errorf("notes = %q", l.Notes)
	}

	l, _ = c.Start(Brief{Slug: "new-thing", Role: "briefing", Request: "fe issue 45"})
	if strings.Join(l.Args[3:5], " ") != "--name ws new-thing · briefing" || strings.Contains(strings.Join(l.Args, " "), "--model") {
		t.Errorf("briefing args = %q", l.Args)
	}
	if !strings.Contains(l.Args[len(l.Args)-1], "You are briefing a new mission") {
		t.Errorf("briefing prompt = %q", l.Args[len(l.Args)-1])
	}
}

func TestClaude_ResumeAndAvailability(t *testing.T) {
	t.Setenv(EnvClaudeBin, "")
	c := fakeClaude(map[string]string{"claude": "/bin/claude"})
	l, err := c.Resume("/w", "abc")
	if err != nil || strings.Join(l.Args, " ") != "/bin/claude --resume abc" || l.Dir != "/w" || l.Conversation != "abc" {
		t.Errorf("resume = %+v, %v", l, err)
	}
	if _, err := c.Resume("/w", ""); err == nil {
		t.Error("resume without a session id should fail")
	}
	if err := fakeClaude(nil).Available(); err == nil || !strings.Contains(err.Error(), EnvClaudeBin) {
		t.Errorf("missing claude: %v", err)
	}
	t.Setenv(EnvClaudeBin, "/opt/claude-dev")
	if b, err := fakeClaude(map[string]string{"/opt/claude-dev": "/opt/claude-dev"}).bin(); err != nil || b != "/opt/claude-dev" {
		t.Errorf("override bin = %q, %v", b, err)
	}
}

// The orchestrator runs headless with only ws mission/lift and read-only
// tools allowed; dontAsk refuses everything else without a prompt.
func TestClaude_Orchestrate(t *testing.T) {
	t.Setenv(EnvClaudeBin, "")
	l, err := fakeClaude(map[string]string{"claude": "/bin/claude"}).Orchestrate(OrchestratorRun{
		Prompt: "Read the prompt file.", Dir: "/root", Model: "haiku",
		Capsules: []string{"/root/repos/api/fix", "/root/repos/odd path/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(l.Args, " ")
	for _, want := range []string{
		"/bin/claude -p --output-format text --permission-mode dontAsk --no-session-persistence --model haiku --allowedTools ",
		"Bash(ws mission *)", "Bash(ws lift *)", "Bash(git diff *)", "Bash(gh pr checks *)",
		"Bash(git -C /root/repos/api/fix log *)", "Bash(git -C /root/repos/api/fix status *)",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	// No * before a git subcommand (git -C * log * would also match
	// git -C x -c core.fsmonitor=<cmd> log), and no path that can't be
	// written into a rule exactly.
	for _, never := range []string{"--dangerously-skip-permissions", "bypassPermissions", "Bash(ws *)", "Edit", "Write", "git -C * ", "odd path"} {
		if strings.Contains(args, never) {
			t.Errorf("args shouldn't contain %q: %s", never, args)
		}
	}
	// -- ends --allowedTools' list, so the prompt isn't taken for a rule.
	if n := len(l.Args); l.Args[n-2] != "--" || l.Args[n-1] != "Read the prompt file." || l.Dir != "/root" {
		t.Errorf("tail = %q, dir %q", l.Args[n-2:], l.Dir)
	}
}

func TestCursor_ModelAndOrchestratorDefault(t *testing.T) {
	t.Setenv(EnvCursorBin, "")
	c := fakeCursor(map[string]string{"cursor-agent": "/bin/ca"}, "abc123def\n", nil)
	l, _ := c.Start(Brief{Slug: "x", Model: "gpt-5.5-high"})
	if strings.Join(l.Args, " ") != "/bin/ca --trust --resume abc123def --model gpt-5.5-high" {
		t.Errorf("args = %q", l.Args)
	}
	if c.OrchestratorModel() != CursorOrchestratorModel {
		t.Errorf("orchestrator model = %q", c.OrchestratorModel())
	}
}

func TestNewSessionID(t *testing.T) {
	a, err := newSessionID()
	b, _ := newSessionID()
	shape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if err != nil || !shape.MatchString(a) || a == b {
		t.Errorf("ids %q %q, %v", a, b, err)
	}
}

func TestBriefingPrompt(t *testing.T) {
	p := InitialPrompt(Brief{Slug: "fe-issue-45", Role: "briefing", Request: "fe issue 45", Org: "acme"})
	for _, want := range []string{
		"NOT doing the work",
		"fe issue 45",
		"acme/<repo>",
		"gh issue view",
		"ws mission brief --set -",
		"ws mission title",
		"ws lift <repo> <branch-name>",
		"ws mission ready",
		"Don't change any code",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("briefing prompt missing %q", want)
		}
	}
	if work := InitialPrompt(Brief{Slug: "x", Title: "X"}); strings.Contains(work, "ws mission ready") {
		t.Error("work crew prompt mentions ws mission ready")
	}
}

func TestWorkPromptTeachesSignals(t *testing.T) {
	p := InitialPrompt(Brief{Slug: "m", Title: "M"})
	for _, want := range []string{"ws mission ask", "ws mission done", "ws mission artifact add", "ws review show -f llm", "commit it"} {
		if !strings.Contains(p, want) {
			t.Errorf("work prompt missing %q", want)
		}
	}
}

// Crew asked to answer the orchestrator's question get the question, not
// the whole brief, and are told to answer with ws mission answer and stop.
func TestAnswerPrompt(t *testing.T) {
	p := InitialPrompt(Brief{
		Slug: "hello", Title: "Hello", Role: "answer",
		Brief:    "## Goal\nA long brief nobody needs here.",
		Question: "Commit and open PRs, or leave them?", Options: []string{"Commit both and open PRs", "Leave them uncommitted"},
	})
	for _, want := range []string{
		"Commit and open PRs, or leave them?", "1. Commit both and open PRs", "2. Leave them uncommitted",
		`ws mission answer "<the answer>"`, "ws mission show -f llm", "Don't do the work the answer leads to",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "A long brief nobody needs here") || strings.Contains(p, "ws mission done") {
		t.Errorf("answer prompt pastes the brief or asks for done:\n%s", p)
	}
}

// ws's hooks go into the one session: Claude Code through an inline
// --settings, Cursor through a plugin folder ws writes.
func TestHookArgs(t *testing.T) {
	cmd := []string{"/w/.ws/bin/ws", "mission", "hook", "--root", "/w s"}
	args, err := (&Claude{}).HookArgs(cmd, t.TempDir())
	if err != nil || len(args) != 2 || args[0] != "--settings" {
		t.Fatalf("claude args = %q, %v", args, err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal([]byte(args[1]), &settings); err != nil {
		t.Fatal(err)
	}
	for event, ours := range map[string]string{"SessionStart": HookStart, "UserPromptSubmit": HookPrompt, "Stop": HookStop, "Notification": HookNotify} {
		h := settings.Hooks[event]
		if len(h) != 1 || h[0].Hooks[0].Type != "command" || h[0].Hooks[0].Command != "/w/.ws/bin/ws mission hook --root '/w s' "+ours {
			t.Errorf("claude %s hook = %+v", event, h)
		}
	}

	dir := t.TempDir()
	args, err = (&Cursor{}).HookArgs(cmd, dir)
	if err != nil || strings.Join(args, " ") != "--plugin-dir "+dir {
		t.Fatalf("cursor args = %q, %v", args, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor-plugin", "plugin.json")); err != nil {
		t.Errorf("no plugin manifest: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "hooks", "hooks.json"))
	var hooks struct {
		Version int
		Hooks   map[string][]struct{ Command string }
	}
	json.Unmarshal(data, &hooks)
	if _, ok := hooks.Hooks["afterAgentResponse"]; ok {
		t.Error("cursor's afterAgentResponse fires for every message, out of order: the reply comes from the transcript at stop")
	}
	for event, ours := range map[string]string{"sessionStart": HookStart, "beforeSubmitPrompt": HookPrompt, "stop": HookStop} {
		if h := hooks.Hooks[event]; hooks.Version != 1 || len(h) != 1 || !strings.HasSuffix(h[0].Command, "'/w s' "+ours) {
			t.Errorf("cursor %s hook = %+v", event, h)
		}
	}
}

// Cursor runs ws's plugin hooks only for events your own hooks file has a
// hook (by path) for: the fix adds do-nothing ones, keeping what's there.
func TestEnableCursorHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	missing, err := CursorHooksMissing()
	if err != nil || strings.Join(missing, ",") != "beforeSubmitPrompt,stop" {
		t.Fatalf("no file: missing %v, %v", missing, err)
	}
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"version":1,"hooks":{"stop":[{"command":"/opt/mine/notify.sh"}],"beforeSubmitPrompt":[{"command":"true"}],"afterFileEdit":[{"command":"/opt/fmt.sh"}]},"extra":"kept"}`), 0o644)
	if missing, _ := CursorHooksMissing(); strings.Join(missing, ",") != "beforeSubmitPrompt" {
		t.Errorf("your stop hook counts, a bare 'true' doesn't: missing %v", missing)
	}
	if err := EnableCursorHooks(); err != nil {
		t.Fatal(err)
	}
	if missing, _ := CursorHooksMissing(); len(missing) != 0 {
		t.Errorf("still missing %v", missing)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`"/opt/mine/notify.sh"`, `"/opt/fmt.sh"`, `"extra": "kept"`, `"command": "true"`, `"command": "/usr/bin/true"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("hooks file lost or lacks %s:\n%s", want, data)
		}
	}
	if strings.Count(string(data), "/usr/bin/true") != 1 {
		t.Errorf("added a no-op where your own stop hook already counts:\n%s", data)
	}
}

// Work crew are taught to leave a handoff, and new crew get where earlier
// crew left off after what they're dispatched for and before the brief.
func TestWorkPrompt_HandoffsInAndOut(t *testing.T) {
	p := InitialPrompt(Brief{Slug: "m", Title: "M", Dispatch: "Add eviction", LeftOff: "## Where earlier crew left off\n\nc1's handoff", Brief: "## Goal\nCache it."})
	for _, want := range []string{"--handoff - <<'EOF'", "## Learned", "## Read first", "Learned matters most"} {
		if !strings.Contains(p, want) {
			t.Errorf("work prompt missing %q", want)
		}
	}
	dispatch, left, brief := strings.Index(p, "Add eviction"), strings.Index(p, "Where earlier crew left off"), strings.Index(p, "Cache it.")
	if dispatch < 0 || left < dispatch || brief < left {
		t.Errorf("want the dispatch, then where earlier crew left off, then the brief:\n%s", p)
	}
	if strings.Contains(InitialPrompt(Brief{Slug: "m", Title: "M"}), "left off") {
		t.Error("left off shown with no handoffs")
	}
}

// Crew on a staged brief are told their stage and to do only that.
func TestWorkPrompt_Stage(t *testing.T) {
	p := InitialPrompt(Brief{Slug: "m", Title: "M", Stage: "Implement it in `api/cache`.", StageN: 2, StageOf: 3})
	for _, want := range []string{"plan has 3 stages", "You're on stage 2:", "Implement it in `api/cache`.", "Do that stage only"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "first stage not yet done") {
		t.Error("told to find its own stage")
	}
	if b := InitialPrompt(Brief{Slug: "b", Role: "briefing", Request: "x"}); !strings.Contains(b, "`repo/name` in backticks") || !strings.Contains(b, "Check in with me.") {
		t.Errorf("briefing prompt doesn't teach the stage format:\n%s", b)
	}
}

func fakePi(found map[string]string) *Pi {
	return &Pi{
		lookPath: func(name string) (string, error) {
			if p, ok := found[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		newID: func() (string, error) { return "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed", nil },
	}
}

// pi crew start on a session id ws chose, with the opening prompt as the
// argument, like Claude Code's.
func TestPi_Start(t *testing.T) {
	t.Setenv(EnvPiBin, "")
	p := fakePi(map[string]string{"pi": "/bin/pi"})
	l, err := p.Start(Brief{Slug: "auth", Title: "Auth", Brief: "Fix it.", Dir: "/w/fe/auth", Model: "anthropic/claude-sonnet-5-5"})
	if err != nil {
		t.Fatal(err)
	}
	const id = "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed"
	want := []string{"/bin/pi", "--session-id", id, "--name", "ws auth", "--model", "anthropic/claude-sonnet-5-5", "--"}
	if len(l.Args) != len(want)+1 || strings.Join(l.Args[:len(want)], " ") != strings.Join(want, " ") {
		t.Fatalf("args = %q", l.Args)
	}
	if prompt := l.Args[len(want)]; !strings.Contains(prompt, "ws mission show -f llm") || !strings.Contains(prompt, "Fix it.") || l.Prompt != "" {
		t.Errorf("prompt arg = %q, typed = %q", prompt, l.Prompt)
	}
	if l.Conversation != id || l.Dir != "/w/fe/auth" || len(l.Notes) != 2 {
		t.Errorf("launch = %+v", l)
	}
	// No --approve: trusting a project loads the extensions it ships, so
	// that's the user's call.
	if strings.Contains(strings.Join(l.Args, " "), "--approve") {
		t.Errorf("args = %q", l.Args)
	}

	l, _ = p.Start(Brief{Slug: "new-thing", Role: "briefing", Request: "fe issue 45"})
	if strings.Join(l.Args[3:5], " ") != "--name ws new-thing · briefing" || strings.Contains(strings.Join(l.Args, " "), "--model") {
		t.Errorf("briefing args = %q", l.Args)
	}
}

func TestPi_ResumeAndAvailability(t *testing.T) {
	t.Setenv(EnvPiBin, "")
	p := fakePi(map[string]string{"pi": "/bin/pi"})
	// --session, not --session-id: that would quietly start a blank session
	// if pi had lost this one.
	l, err := p.Resume("/w", "abc")
	if err != nil || strings.Join(l.Args, " ") != "/bin/pi --session abc" || l.Dir != "/w" || l.Conversation != "abc" {
		t.Errorf("resume = %+v, %v", l, err)
	}
	if _, err := p.Resume("/w", ""); err == nil {
		t.Error("resume without a session id should fail")
	}
	if err := fakePi(nil).Available(); err == nil || !strings.Contains(err.Error(), EnvPiBin) {
		t.Errorf("missing pi: %v", err)
	}
	t.Setenv(EnvPiBin, "/opt/pi-dev")
	if b, err := fakePi(map[string]string{"/opt/pi-dev": "/opt/pi-dev"}).bin(); err != nil || b != "/opt/pi-dev" {
		t.Errorf("override bin = %q, %v", b, err)
	}
}

// The orchestrator runs in print mode without a saved session, reading and
// running commands but not editing; with no model configured, on pi's own
// default.
func TestPi_Orchestrate(t *testing.T) {
	t.Setenv(EnvPiBin, "")
	p := fakePi(map[string]string{"pi": "/bin/pi"})
	l, err := p.Orchestrate(OrchestratorRun{Prompt: "Read the prompt file.", Dir: "/root", Model: "haiku"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(l.Args, " "); got != "/bin/pi -p --no-session --tools read,bash,grep,find,ls --model haiku -- Read the prompt file." || l.Dir != "/root" {
		t.Errorf("args = %q, dir %q", got, l.Dir)
	}
	if p.OrchestratorModel() != "" {
		t.Errorf("orchestrator model = %q", p.OrchestratorModel())
	}
	pick, o, err := (Roles{Crew: Pick{Harness: "pi", Model: "sonnet"}}).ForOrchestrator()
	if err != nil || o.Name() != "pi" || pick != (Pick{Harness: "pi"}) {
		t.Errorf("pi crew's orchestrator = %+v, %v", pick, err)
	}
	l, _ = p.Orchestrate(OrchestratorRun{Prompt: "x", Dir: "/root"})
	if strings.Contains(strings.Join(l.Args, " "), "--model") {
		t.Errorf("no model configured: args = %q", l.Args)
	}
}

// ws's hooks go into a pi session as an extension ws writes, which runs the
// hook command with each of ws's events.
func TestPi_HookArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hooks", "pi")
	cmd := []string{"/w/.ws/bin/ws", "mission", "hook", "--root", "/w s"}
	args, err := (&Pi{}).HookArgs(cmd, dir)
	path := filepath.Join(dir, "ws-crew-hooks.ts")
	if err != nil || strings.Join(args, " ") != "--extension "+path {
		t.Fatalf("args = %q, %v", args, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	// The command is a JSON array, run without a shell.
	if !strings.Contains(src, `const command: string[] = ["/w/.ws/bin/ws","mission","hook","--root","/w s"];`) {
		t.Errorf("command missing:\n%s", src)
	}
	for event, ours := range map[string]string{
		"session_start": HookStart, "before_agent_start": HookPrompt,
		"agent_settled": HookStop, "ui_prompt_start": HookNotify,
	} {
		re := regexp.MustCompile(`pi\.on\("` + event + `",[^}]*?send\("` + ours + `"`)
		if !re.MatchString(src) {
			t.Errorf("no %s hook sending %q", event, ours)
		}
	}
	if strings.Contains(src, "$") {
		t.Errorf("a placeholder wasn't filled in:\n%s", src)
	}
	// agent_end isn't the end of a turn: pi may still retry or go on.
	if strings.Contains(src, `"agent_end"`) {
		t.Error("stop should come from agent_settled, not agent_end")
	}
	if _, err := (&Pi{}).HookArgs(nil, dir); err == nil {
		t.Error("no command should fail")
	}
}
