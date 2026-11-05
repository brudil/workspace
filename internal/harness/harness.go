// Package harness knows how to launch and resume each kind of agent CLI
// (Cursor, Claude Code, a plain shell) in the roles agents play on a
// mission: crew doing the work, briefing a new mission, and the headless
// orchestrator. Roles says which harness and model each role uses.
//
// A harness only builds commands. Running them in a terminal pane, setting
// WS_MISSION/WS_CREW, and recording crew are the deck's job.
package harness

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// EnvDefault picks the harness crew run, over [crew] harness in config.
const EnvDefault = "WS_DECK_HARNESS"

// Brief is what a harness needs to know about the mission it starts on.
type Brief struct {
	Slug  string
	Title string
	Brief string // brief.md contents
	Dir   string // working directory (a mission capsule, or the workspace root when briefing)
	// Model is the model to run, or "" for the agent's own default.
	Model string

	// Role is "" for crew working on the mission, or "briefing" for crew
	// turning a request into a brief and capsules.
	Role    string
	Request string // what the user asked for (briefing)
	// Handoff is what this crew was dispatched to do: its part of the
	// mission, and what earlier crew left for it.
	Handoff string
	Org     string // GitHub org the workspace's repos live in
}

// Launch is a ready-to-run agent command.
type Launch struct {
	Args []string // argv, Args[0] is the program
	Dir  string
	// Conversation is the agent's own session id when known before the
	// agent starts, so the crew record can be resumed later.
	Conversation string
	// Prompt, when set, is pasted into the agent once its interface has
	// drawn and settled, then submitted with Enter, as a person would.
	// Typing it in avoids relying on how a CLI treats a long multi-line
	// prompt argument.
	Prompt string
	// Notes say what the harness ran getting ready (and how it went), for
	// the exec log: a chat id fetched, or why there isn't one.
	Notes []string
}

// Harness launches one kind of agent.
type Harness interface {
	Name() string
	// Available reports why the harness can't run here, or nil.
	Available() error
	// Start builds the command for new crew on a mission.
	Start(b Brief) (Launch, error)
	// Resume builds the command to continue an earlier conversation. It
	// returns ErrNoResume if the harness can't.
	Resume(dir, conversation string) (Launch, error)
}

// ErrNoResume is returned by harnesses that can't resume a conversation.
var ErrNoResume = errors.New("this harness can't resume conversations")

var registry = map[string]Harness{}

func register(h Harness) { registry[h.Name()] = h }

// Register adds a harness (or replaces one of the same name). Built-in
// harnesses register themselves; tests use this for fakes.
func Register(h Harness) { register(h) }

// Get returns a harness by name.
func Get(name string) (Harness, error) {
	if h, ok := registry[name]; ok {
		return h, nil
	}
	return nil, fmt.Errorf("unknown harness %q (have %s)", name, strings.Join(Names(), ", "))
}

// Names lists the registered harnesses.
func Names() []string {
	var names []string
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// InitialPrompt is the first message given to new crew. It points the agent at
// the mission rather than pasting everything, so the brief stays the single
// source of truth, but includes the brief too so the agent can start at once.
func InitialPrompt(b Brief) string {
	if b.Role == "briefing" {
		return briefingPrompt(b)
	}
	var s strings.Builder
	fmt.Fprintf(&s, "You are crew on the mission %q (%s), managed by the ws workspace tool.\n\n", b.Title, b.Slug)
	s.WriteString("Before starting, run `ws mission show -f llm` to see the mission's capsules (worktrees, possibly across repos) and log. ")
	s.WriteString("Work only in this mission's capsules; create new ones with `ws lift <repo> <name>` and they join the mission automatically. ")
	s.WriteString("Record significant decisions and progress with `ws mission note \"...\"`. Don't edit the brief.\n\n")
	if h := strings.TrimSpace(b.Handoff); h != "" {
		s.WriteString("You were dispatched for one part of this mission. Your handoff:\n\n")
		s.WriteString(h)
		s.WriteString("\n\nDo that part, then report done; ws's orchestrator decides what follows and dispatches it.\n\n")
	} else {
		s.WriteString("If the brief describes the work in stages for separate crew, do only the first stage not yet done, then report done; ws's orchestrator dispatches the next.\n\n")
	}
	s.WriteString("If you notice something worth doing outside this mission, don't do it: run `ws mission new \"<what it is and why>\"` (plain text; it waits for the user to brief it) and mention it in one line.\n\n")
	s.WriteString("Tell the user's deck where you are, so they know when you need them:\n")
	s.WriteString("- When you need a decision from the user, ask in your reply and also run `ws mission ask \"<the question>\"`.\n")
	s.WriteString("- When you make something the next crew or the user will need (a proposal, an ADR, notes, a doc), record it as you go: `ws mission artifact add <file> --kind <proposal|adr|note|doc> \"<label>\"` (a file in a capsule, in a .ground, or in the mission's private files folder; or a URL). `ws mission show -f llm` lists the mission's artifacts with where to read each, the private files folder, and the records repo for shared proposals and ADRs, if the workspace has one.\n")
	s.WriteString("- When your part is finished (PRs opened, if that's how this repo works), run `ws mission done \"<one-line summary>\"`. ws finds PRs from the mission's capsules itself.\n")
	if brief := strings.TrimSpace(b.Brief); brief != "" {
		s.WriteString("\nThe brief:\n\n")
		s.WriteString(brief)
		s.WriteString("\n")
	}
	return s.String()
}

// briefingPrompt sets up crew whose only job is to turn a request into a
// ready mission (brief and capsules) and then hand over.
func briefingPrompt(b Brief) string {
	var s strings.Builder
	s.WriteString("You are briefing a new mission in a multi-repo workspace managed by the ws tool. ")
	s.WriteString("You are NOT doing the work: another agent will, starting from what you set up. ")
	s.WriteString("Your job is to turn the user's request into a ready mission, then stop.\n\n")
	fmt.Fprintf(&s, "The request (mission %s):\n\n%s\n\n", b.Slug, strings.TrimSpace(b.Request))
	s.WriteString("Steps:\n\n")
	s.WriteString("1. Understand the request. Run `ws status -f llm` to see the repos, their aliases and their worktrees. ")
	if b.Org != "" {
		fmt.Fprintf(&s, "Repos live on GitHub as %s/<repo>. ", b.Org)
	}
	s.WriteString("If the request names an issue or PR (\"fe issue 45\", \"api#12\", a URL), read it with `gh issue view` / `gh pr view` (use `--repo <org>/<repo>` and `--comments`). ")
	s.WriteString("Read code in a repo's `.ground` worktree (read-only) or with `ws rg`. Don't change any code.\n")
	s.WriteString("2. Ask the user about anything ambiguous: which repos, scope, what done looks like.\n")
	s.WriteString("3. Write the brief, replacing it whole:\n\n")
	s.WriteString("   ws mission brief --set - <<'EOF'\n   ## Goal\n   ...\n   ## Context\n   (links to issues/PRs, relevant files)\n   ## Constraints\n   ...\n   ## Acceptance\n   ...\n   EOF\n\n")
	s.WriteString("   If the work falls into stages that separate crew should do one after another (a proposal, then its implementation), list them in order under `## Stages`, saying where each one's output goes and where the user wants to check in before the next. ws's orchestrator reads the brief and dispatches crew stage by stage.\n")
	s.WriteString("4. Name it: `ws mission title \"...\"`. A terse, plain-English name of at most 50 characters (it's how the mission reads in the user's deck), not the request or an issue number: \"Cache token lookups\", not \"fe issue 45\".\n")
	s.WriteString("5. Create the capsules (worktrees) the work needs, one per repo it touches: `ws lift <repo> <branch-name>` for new work, or `ws dock <repo> <branch|PR>` to build on an existing branch or PR. They join this mission automatically. Use a short descriptive branch name. If a stage produces a proposal, ADR or note to share and `ws mission show -f llm` names a records repo, that stage gets a capsule there too.\n")
	s.WriteString("6. Show the user the brief and capsules (`ws mission show -f llm`) and check they're right.\n")
	s.WriteString("7. When they agree, run `ws mission ready` (add `--slug <short-slug>` if the current slug is poor). That marks the mission ready and ends this session; work crew is launched from the deck.\n")
	return s.String()
}

// Orchestrator is a harness that can run a one-shot, headless agent: a
// prompt in, its final message out, using tools (shell) along the way.
type Orchestrator interface {
	Harness
	// Orchestrate builds the headless command.
	Orchestrate(r OrchestratorRun) (Launch, error)
	// OrchestratorModel is the model it runs the orchestrator on when
	// none is configured: small and quick, it only decides what's next.
	OrchestratorModel() string
}

// OrchestratorRun is one headless orchestrator run.
type OrchestratorRun struct {
	Prompt string // what's passed: one line pointing at the prompt file
	Dir    string // where it runs (the workspace root)
	Model  string // "" for the agent's own default
	// Capsules are the paths of the mission's capsules, which it may
	// inspect read-only (git -C <path> log, …).
	Capsules []string
}
