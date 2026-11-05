package harness

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Claude runs Claude Code (the claude CLI).
//
// Every assumption about the Claude Code CLI lives in this file. Those
// checked against a real claude (2.1.318) say VERIFIED.
type Claude struct {
	// lookPath and newID are swapped in tests.
	lookPath func(string) (string, error)
	newID    func() (string, error)
}

// EnvClaudeBin overrides the Claude Code binary (name or path).
const EnvClaudeBin = "WS_CLAUDE_BIN"

func init() { register(&Claude{lookPath: exec.LookPath, newID: newSessionID}) }

func (c *Claude) Name() string { return "claude" }

func (c *Claude) bin() (string, error) {
	name := "claude"
	if b := os.Getenv(EnvClaudeBin); b != "" {
		name = b
	}
	p, err := c.lookPath(name)
	if err != nil {
		return "", fmt.Errorf("claude CLI not found (looked for %s; set %s to override)", name, EnvClaudeBin)
	}
	return p, nil
}

func (c *Claude) Available() error {
	_, err := c.bin()
	return err
}

// Start builds new crew (or a briefing agent).
//
// VERIFIED: --session-id <uuid> names the conversation before it starts, so
// ws knows what to resume without asking the CLI for an id.
//
// VERIFIED: the opening prompt goes in as the argument, not typed in. In a
// folder Claude Code hasn't been trusted in, it opens on a trust dialog whose
// default answer is "No, exit": a typed-in prompt's Enter would choose that.
// A prompt argument waits for the user's answer and runs after it. The
// session stays interactive afterwards.
//
// VERIFIED: in a git repo, trust is per repository. Trusting one capsule
// covered that repo's later capsules (worktrees of the same repo), but
// trusting the workspace root (the config repo) didn't cover the repos
// under it, so the first crew in each repo asks once. Until someone answers,
// that crew shows as waiting in the deck.
//
// VERIFIED: --name sets the session's display name, which is also the
// terminal title the deck shows.
func (c *Claude) Start(b Brief) (Launch, error) {
	bin, err := c.bin()
	if err != nil {
		return Launch{}, err
	}
	id, err := c.newID()
	if err != nil {
		return Launch{}, fmt.Errorf("making a session id: %w", err)
	}
	args := []string{bin, "--session-id", id, "--name", sessionName(b)}
	if b.Model != "" {
		args = append(args, "--model", b.Model)
	}
	prompt := InitialPrompt(b)
	args = append(args, "--", prompt)
	return Launch{Args: args, Dir: b.Dir, Conversation: id, Notes: []string{
		fmt.Sprintf("chose session id %s (--session-id)", id),
		fmt.Sprintf("opening prompt is its last argument (%d chars):\n%s", len(prompt), prompt),
	}}, nil
}

// sessionName is how the session reads in claude's /resume picker and the
// terminal title.
func sessionName(b Brief) string {
	if b.Role == "briefing" {
		return "ws " + b.Slug + " · briefing"
	}
	return "ws " + b.Slug
}

// VERIFIED: --resume <session-id> reopens the conversation interactively,
// history and all.
func (c *Claude) Resume(dir, conversation string) (Launch, error) {
	if conversation == "" {
		return Launch{}, fmt.Errorf("no claude session id recorded for this crew, so it can't be resumed")
	}
	bin, err := c.bin()
	if err != nil {
		return Launch{}, err
	}
	return Launch{Args: []string{bin, "--resume", conversation}, Dir: dir, Conversation: conversation}, nil
}

// ClaudeOrchestratorModel is the model Claude Code runs the orchestrator on
// unless [orchestrator] model says otherwise (an alias for the latest).
const ClaudeOrchestratorModel = "haiku"

func (c *Claude) OrchestratorModel() string { return ClaudeOrchestratorModel }

// claudeOrchestratorTools is all the orchestrator may use. It acts through
// ws mission (and ws lift for a capsule the next crew needs) and looks around
// read-only; anything else is refused without a prompt (dontAsk), and the
// refusal comes back to it as an error it can correct.
//
// VERIFIED: a chained command (a && b; c | d) runs if every part is allowed,
// except cd <dir> && git …, which Claude Code always refuses. One refused
// command reads to a model as "Bash is denied" (that's how the refusal is
// worded): it stops using the shell and never runs its ws commands. So the
// harmless helpers models chain by habit are allowed, and the prompt asks
// for one command per call and git -C <capsule path> (see capsuleGitRules).
var claudeOrchestratorTools = []string{
	"Read", "Grep", "Glob",
	"Bash(ws mission *)", "Bash(workspace mission *)",
	"Bash(ws lift *)", "Bash(workspace lift *)",
	"Bash(ws status *)", "Bash(workspace status *)",
	"Bash(git diff *)", "Bash(git log *)", "Bash(git show *)", "Bash(git status *)",
	"Bash(gh pr view *)", "Bash(gh pr checks *)", "Bash(gh pr list *)",
	"Bash(cd *)", "Bash(pwd)", "Bash(ls *)", "Bash(cat *)", "Bash(head *)", "Bash(tail *)", "Bash(wc *)", "Bash(echo *)",
}

// capsuleGitRules allows read-only git in each capsule by its exact path.
//
// VERIFIED: a * before the git subcommand (Bash(git -C * log *)) would also
// match options put in its place, and git -c / --exec-path run arbitrary
// commands; Claude Code warns about such rules. With the path spelled out,
// * only covers what follows the subcommand, where -c can't go.
func capsuleGitRules(paths []string) []string {
	var rules []string
	for _, p := range paths {
		if p == "" || strings.ContainsAny(p, " \t\n\"'()*") {
			continue // can't be written into a rule exactly; cd and plain git still work
		}
		for _, sub := range []string{"diff", "log", "show", "status"} {
			rules = append(rules, "Bash(git -C "+p+" "+sub+" *)")
		}
	}
	return rules
}

// Orchestrate runs a headless orchestrator in print mode.
//
// VERIFIED: -p runs one prompt and prints the final message (--output-format
// text), skipping the trust dialog. --permission-mode dontAsk with
// --allowedTools runs what's allowed and refuses the rest without asking;
// --allowedTools takes one rule per argument, so -- ends the list before the
// prompt. --no-session-persistence keeps these runs out of the user's
// /resume list (their output is in orchestrator.log).
func (c *Claude) Orchestrate(r OrchestratorRun) (Launch, error) {
	bin, err := c.bin()
	if err != nil {
		return Launch{}, err
	}
	args := []string{bin, "-p", "--output-format", "text", "--permission-mode", "dontAsk", "--no-session-persistence"}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	args = append(args, "--allowedTools")
	args = append(args, claudeOrchestratorTools...)
	args = append(args, capsuleGitRules(r.Capsules)...)
	return Launch{Args: append(args, "--", r.Prompt), Dir: r.Dir}, nil
}

// newSessionID makes a random (version 4) UUID, the form --session-id takes.
func newSessionID() (string, error) {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return "", err
	}
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return strings.ToLower(fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])), nil
}
