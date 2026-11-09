package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Pi runs the pi coding agent (the pi CLI).
//
// Every assumption about the pi CLI lives in this file. Those checked
// against a real pi (1.5.0) say VERIFIED.
type Pi struct {
	// lookPath and newID are swapped in tests.
	lookPath func(string) (string, error)
	newID    func() (string, error)
}

// EnvPiBin overrides the pi binary (name or path).
const EnvPiBin = "WS_PI_BIN"

func init() { register(&Pi{lookPath: exec.LookPath, newID: newSessionID}) }

func (p *Pi) Name() string { return "pi" }

func (p *Pi) bin() (string, error) {
	name := "pi"
	if b := os.Getenv(EnvPiBin); b != "" {
		name = b
	}
	path, err := p.lookPath(name)
	if err != nil {
		return "", fmt.Errorf("pi CLI not found (looked for %s; set %s to override)", name, EnvPiBin)
	}
	return path, nil
}

func (p *Pi) Available() error {
	_, err := p.bin()
	return err
}

// Start builds new crew (or a briefing agent).
//
// VERIFIED: --session-id <id> names the conversation before it starts
// (creating it in the current directory), so ws knows what to resume
// without asking the CLI for an id. A UUID is a valid id.
//
// VERIFIED: the opening prompt goes in as the argument, after --, and the
// session stays interactive afterwards. In a folder with project-local pi
// resources (.pi/settings.json, .pi/extensions, …) pi first asks whether
// to trust them, and the prompt runs once that's answered. ws doesn't
// answer it for you: trusting a project loads the extensions it ships.
//
// VERIFIED: --name sets the session's display name, as in pi's /resume
// picker.
func (p *Pi) Start(b Brief) (Launch, error) {
	bin, err := p.bin()
	if err != nil {
		return Launch{}, err
	}
	id, err := p.newID()
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

// Resume reopens the conversation interactively, history and all.
//
// VERIFIED: --session <id> opens the session with that id and runs it in
// the folder it was started in. Unlike --session-id, which would start a
// blank session of that id if pi has lost it, it fails when there's none.
func (p *Pi) Resume(dir, conversation string) (Launch, error) {
	if conversation == "" {
		return Launch{}, fmt.Errorf("no pi session id recorded for this crew, so it can't be resumed")
	}
	bin, err := p.bin()
	if err != nil {
		return Launch{}, err
	}
	return Launch{Args: []string{bin, "--session", conversation}, Dir: dir, Conversation: conversation}, nil
}

// OrchestratorModel is "": pi runs on whichever providers you've logged in
// to, so ws can't name a small model that's sure to exist. The orchestrator
// runs on your pi default unless [orchestrator] model names one.
func (p *Pi) OrchestratorModel() string { return "" }

// piOrchestratorTools are the built-in tools the orchestrator gets: reading
// and the shell, but not edit or write. pi has no permission prompts, so
// bash can run anything; like Cursor's, it relies on being told to act only
// through ws.
const piOrchestratorTools = "read,bash,grep,find,ls"

// Orchestrate runs a headless orchestrator in print mode.
//
// VERIFIED: -p runs one prompt and prints the final message (text mode is
// the default). --no-session keeps these runs out of pi's sessions (their
// output is in orchestrator.log). --tools limits it to the tools named.
// Project-local pi resources aren't loaded unless you've trusted the
// workspace root: print mode can't ask.
func (p *Pi) Orchestrate(r OrchestratorRun) (Launch, error) {
	bin, err := p.bin()
	if err != nil {
		return Launch{}, err
	}
	args := []string{bin, "-p", "--no-session", "--tools", piOrchestratorTools}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	return Launch{Args: append(args, "--", r.Prompt), Dir: r.Dir}, nil
}

// piHooksFile is the extension ws writes into the hooks folder.
const piHooksFile = "ws-crew-hooks.ts"

// HookArgs installs ws's hooks in this pi session alone, as an extension
// written into dir and loaded with --extension: it adds to your own.
//
// VERIFIED (pi 1.5.0): an --extension file loads alongside discovered ones,
// and runs in pi's process, with the session's environment (WS_MISSION,
// WS_CREW). session_start (getSessionId()), before_agent_start (also for
// the opening prompt given as the argument), message_end (each message,
// the assistant's with its text) and agent_settled (pi won't go on by
// itself: retries, compaction and queued messages are over) fire in that
// order for a turn. agent_end isn't the end: pi may still continue.
// ui_prompt_start fires when an extension's dialog (a confirm, a select)
// waits for you; pi asks no permission of its own.
func (p *Pi) HookArgs(cmd []string, dir string) ([]string, error) {
	src, err := piHooksExtension(cmd)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("writing pi hooks: %w", err)
	}
	path := filepath.Join(dir, piHooksFile)
	// Atomically: crew launching at once write the same file, and a session
	// starting must never read half of it.
	if err := writeAtomically(path, []byte(src)); err != nil {
		return nil, fmt.Errorf("writing pi hooks: %w", err)
	}
	return []string{"--extension", path}, nil
}

// piHooksExtension is the extension's source: it runs cmd with ws's hook
// event and a JSON payload on stdin, one at a time in the order they
// happened, without making pi wait. A hook that fails is ignored.
func piHooksExtension(cmd []string) (string, error) {
	quoted := func(v any) string {
		data, _ := json.Marshal(v)
		return string(data)
	}
	if len(cmd) == 0 {
		return "", fmt.Errorf("no hook command")
	}
	r := strings.NewReplacer(
		"$COMMAND", quoted(cmd),
		"$START", quoted(HookStart),
		"$PROMPT", quoted(HookPrompt),
		"$STOP", quoted(HookStop),
		"$NOTIFY", quoted(HookNotify),
	)
	return r.Replace(piHooksTemplate), nil
}

const piHooksTemplate = `// Written by ws (the workspace tool) for the crew it launches in pi: tells
// ws what this crew is doing by running its hook command. Loaded with
// --extension for ws's sessions only; ws rewrites it at every launch.
import { spawn } from "node:child_process";

const command: string[] = $COMMAND;

export default function (pi: any) {
	let queue: Promise<void> = Promise.resolve();
	const send = (event: string, payload: object) => {
		queue = queue.then(
			() =>
				new Promise<void>((done) => {
					try {
						const child = spawn(command[0], [...command.slice(1), event], {
							stdio: ["pipe", "ignore", "ignore"],
						});
						child.on("error", () => done());
						child.on("close", () => done());
						child.stdin.on("error", () => {});
						child.stdin.end(JSON.stringify(payload));
					} catch {
						done();
					}
				}),
		);
	};

	// The last thing the agent said in this run, for the stop event.
	let reply = "";

	pi.on("session_start", (_event: any, ctx: any) => {
		send($START, { session_id: ctx.sessionManager.getSessionId() });
	});
	pi.on("before_agent_start", () => {
		reply = "";
		send($PROMPT, {});
	});
	pi.on("message_end", (event: any) => {
		const message = event.message;
		if (message?.role !== "assistant" || !Array.isArray(message.content)) return;
		const text = message.content
			.filter((part: any) => part?.type === "text" && typeof part.text === "string")
			.map((part: any) => part.text)
			.join("\n")
			.trim();
		if (text) reply = text;
	});
	pi.on("agent_settled", () => {
		send($STOP, { last_assistant_message: reply });
	});
	pi.on("ui_prompt_start", (event: any) => {
		send($NOTIFY, { notification_type: "ui_prompt", message: event.title ?? "" });
	});
}
`
