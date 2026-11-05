// Package execlog records the processes ws starts for missions (crew, the
// orchestrator, the deck server) and what they're sent, in
// <root>/.ws/exec.log: the command line, where it ran, the mission
// environment it got, and how it ended. Every ws process for a workspace
// appends to the same file, so one tail shows the whole story.
package execlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxBytes is when the log rolls over to exec.log.1.
	maxBytes = 10 << 20
	// argMax clips each argument; the rest is counted, not kept.
	argMax = 600
	// noteMax clips text sent to a process (a prompt typed in).
	noteMax = 2000
)

// Path is where a workspace's log is.
func Path(root string) string { return filepath.Join(root, ".ws", "exec.log") }

// Start records a process starting: who it is ("crew cache/c2",
// "orchestrator cache"), its command line (as a shell would take it), where
// it runs, and the mission parts of its environment.
func Start(root, who string, args []string, dir string, env []string, pid int) {
	var b strings.Builder
	fmt.Fprintf(&b, "start")
	if pid != 0 {
		fmt.Fprintf(&b, " pid %d", pid)
	}
	if dir != "" {
		fmt.Fprintf(&b, " in %s", dir)
	}
	fmt.Fprintf(&b, "\n$ %s", CommandLine(args))
	if e := missionEnv(env); e != "" {
		fmt.Fprintf(&b, "\nenv %s", e)
	}
	write(root, who, b.String())
}

// End records how a process ended. took is 0 when unknown.
func End(root, who string, pid int, took time.Duration, result string) {
	var b strings.Builder
	b.WriteString("end")
	if pid != 0 {
		fmt.Fprintf(&b, " pid %d", pid)
	}
	if took > 0 {
		fmt.Fprintf(&b, " after %s", took.Round(100*time.Millisecond))
	}
	b.WriteString(": " + result)
	write(root, who, b.String())
}

// Note records anything else worth knowing: text sent to a process, a
// hang-up, why something didn't run.
func Note(root, who, format string, a ...any) {
	write(root, who, clip(fmt.Sprintf(format, a...), noteMax))
}

// CommandLine renders args so the line can be pasted into a shell, with
// long arguments clipped.
func CommandLine(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = quote(clip(a, argMax))
	}
	return strings.Join(parts, " ")
}

func quote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:,@%+", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("…(+%d bytes)", len(s)-cut)
}

// missionEnv is the part of an environment that explains a run: ws's own
// variables, the terminal, and where ws is found on PATH. Never the rest:
// it holds tokens.
func missionEnv(env []string) string {
	var out []string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "WS_"), k == "TERM", k == "COLORTERM":
			out = append(out, kv)
		case k == "PATH":
			first, _, more := strings.Cut(v, string(os.PathListSeparator))
			if more {
				first += string(os.PathListSeparator) + "…"
			}
			out = append(out, "PATH="+first)
		}
	}
	return strings.Join(out, " ")
}

// write appends one entry in a single write, so entries from several
// processes don't interleave.
func write(root, who, text string) {
	if root == "" {
		return
	}
	path := Path(root)
	if st, err := os.Stat(path); err == nil && st.Size() > maxBytes {
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	// Continuation lines are indented under the entry they belong to.
	f.WriteString(fmt.Sprintf("%s [%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), who, strings.ReplaceAll(text, "\n", "\n    ")))
}
