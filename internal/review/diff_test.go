package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/testgit"
)

func TestMain(m *testing.M) {
	cleanup := testgit.NoSigning()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// capsule makes a repo whose origin/main is the base, with a branch that
// changes files every way a review meets them.
func capsule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "--initial-branch=main")
	run(t, dir, "config", "user.name", "Test")
	run(t, dir, "config", "user.email", "test@example.com")
	var long strings.Builder
	for i := 1; i <= 40; i++ {
		long.WriteString("line " + string(rune('0'+i%10)) + "\n")
	}
	write(t, dir, "cache.go", "package cache\n\nfunc key(id string) string {\n\treturn id\n}\n")
	write(t, dir, "long.txt", long.String())
	write(t, dir, "gone.txt", "bye\n")
	write(t, dir, "old name.md", "# Notes\n\nkeep me\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	run(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")

	run(t, dir, "checkout", "-q", "-b", "cache-tokens")
	write(t, dir, "cache.go", "package cache\n\nfunc key(tenant, id string) string {\n\treturn tenant + \"/\" + id\n}\n")
	write(t, dir, "long.txt", strings.Replace(long.String(), "line 0\n", "line ten\n", 1))
	write(t, dir, "added.go", "package cache\n\n// --- not a header\nvar x = 1")
	os.Remove(filepath.Join(dir, "gone.txt"))
	run(t, dir, "mv", "old name.md", "new name.md")
	write(t, dir, "logo.bin", "\x00\x01\x02binary")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "Key the cache by tenant")
	write(t, dir, "added.go", "package cache\n\n--- not a header\nvar x = 2\n")
	run(t, dir, "commit", "-q", "-am", "Fix the var")
	write(t, dir, "scratch.txt", "not committed\n")
	return dir
}

func file(d *Diff, path string) *File {
	for i := range d.Files {
		if d.Files[i].Path == path {
			return &d.Files[i]
		}
	}
	return nil
}

func TestCompare(t *testing.T) {
	dir := capsule(t)
	d, err := Compare(dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if d.Base != run(t, dir, "rev-parse", "origin/main") || d.Head != run(t, dir, "rev-parse", "HEAD") || d.BaseRef != "origin/main" {
		t.Errorf("base %s head %s", d.Base, d.Head)
	}
	if len(d.Commits) != 2 || d.Commits[0].Subject != "Key the cache by tenant" || d.Commits[1].Author != "Test" || d.Commits[1].Time.IsZero() {
		t.Errorf("commits = %+v", d.Commits)
	}
	if strings.Join(d.Uncommitted, ",") != "scratch.txt" {
		t.Errorf("uncommitted = %q", d.Uncommitted)
	}

	got := map[string]string{}
	for _, f := range d.Files {
		got[f.Path] = f.Status
	}
	want := map[string]string{"cache.go": "modified", "long.txt": "modified", "added.go": "added", "gone.txt": "deleted", "new name.md": "renamed", "logo.bin": "added"}
	for p, s := range want {
		if got[p] != s {
			t.Errorf("%s: %q, want %q (all: %v)", p, got[p], s, got)
		}
	}
	if len(d.Files) != len(want) {
		t.Errorf("files = %v", got)
	}

	c := file(d, "cache.go")
	if c.Additions != 2 || c.Deletions != 2 || len(c.Hunks) != 1 || c.OldBlob == "" || c.NewBlob == "" || len(c.NewBlob) != 40 {
		t.Fatalf("cache.go = %+v", c)
	}
	h := c.Hunks[0]
	if h.OldStart != 1 || h.NewStart != 1 || h.OldLines != 5 || h.NewLines != 5 {
		t.Errorf("hunk = %+v", h)
	}
	var kinds []string
	for _, l := range h.Lines {
		kinds = append(kinds, l.Kind)
	}
	if strings.Join(kinds, "") != "ccddaac" {
		t.Errorf("kinds = %v", kinds)
	}
	if l := h.Lines[4]; l.Kind != "a" || l.New != 3 || l.Old != 0 || l.Text != "func key(tenant, id string) string {" {
		t.Errorf("added line = %+v", l)
	}
	if l := h.Lines[6]; l.Old != 5 || l.New != 5 || l.Text != "}" {
		t.Errorf("context line = %+v", l)
	}

	// A change deep in a file: the hunk starts where it is, with context.
	if lt := file(d, "long.txt"); len(lt.Hunks) != 1 || lt.Hunks[0].OldStart != 7 || lt.Hunks[0].Lines[4].Text != "line ten" {
		t.Errorf("long.txt = %+v", lt.Hunks)
	}

	// A line reading "--- …" inside a hunk is a line, and a file without a
	// final newline says so.
	a := file(d, "added.go")
	if a.OldBlob != "" || len(a.Hunks) != 1 || a.Hunks[0].Lines[2].Text != "--- not a header" || a.Additions != 4 {
		t.Errorf("added.go = %+v", a)
	}
	if r := file(d, "new name.md"); r.OldPath != "old name.md" || len(r.Hunks) != 0 {
		t.Errorf("rename = %+v", r)
	}
	if g := file(d, "gone.txt"); g.NewBlob != "" || g.Deletions != 1 || g.Hunks[0].Lines[0].Old != 1 {
		t.Errorf("deleted = %+v", g)
	}
	if b := file(d, "logo.bin"); !b.Binary || len(b.Hunks) != 0 {
		t.Errorf("binary = %+v", b)
	}
}

func TestCompare_NoNewlineAtEnd(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "--initial-branch=main")
	run(t, dir, "config", "user.name", "Test")
	run(t, dir, "config", "user.email", "test@example.com")
	write(t, dir, "a.txt", "one\ntwo")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	run(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	write(t, dir, "a.txt", "one\ntwo\n")
	run(t, dir, "commit", "-q", "-am", "newline")
	d, err := Compare(dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	lines := d.Files[0].Hunks[0].Lines
	if len(lines) != 3 || lines[1].Kind != "d" || !lines[1].NoEOL || lines[2].Kind != "a" || lines[2].NoEOL {
		t.Errorf("lines = %+v", lines)
	}
}

// A large file's hunks are left out of the review's diff, and come whole
// from FileDiff.
func TestCompare_LargeFile(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "--initial-branch=main")
	run(t, dir, "config", "user.name", "Test")
	run(t, dir, "config", "user.email", "test@example.com")
	write(t, dir, "small.txt", "a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	run(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	write(t, dir, "package-lock.json", strings.Repeat("{\"x\": 1}\n", LargeLines+1))
	write(t, dir, "small.txt", "b\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "deps")
	d, err := Compare(dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	big := file(d, "package-lock.json")
	if !big.Large || len(big.Hunks) != 0 || big.Additions != LargeLines+1 {
		t.Errorf("large = %+v", big.Large)
	}
	if small := file(d, "small.txt"); small.Large || len(small.Hunks) != 1 {
		t.Errorf("small = %+v", small)
	}
	full, err := FileDiff(dir, d.Base, d.Head, "package-lock.json")
	if err != nil || len(full.Hunks) != 1 || len(full.Hunks[0].Lines) != LargeLines+1 {
		t.Errorf("full = %v", err)
	}
	if _, err := FileDiff(dir, d.Base, d.Head, "nope.txt"); err == nil {
		t.Error("an unchanged file should fail")
	}
}

func TestCompare_Errors(t *testing.T) {
	dir := capsule(t)
	if _, err := Compare(dir, "origin/develop"); err == nil || !strings.Contains(err.Error(), "origin/develop") {
		t.Errorf("missing base: %v", err)
	}
	empty := t.TempDir()
	run(t, empty, "init", "-q")
	if _, err := Compare(empty, "origin/main"); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Errorf("no commits: %v", err)
	}
}

func TestBlob(t *testing.T) {
	dir := capsule(t)
	head := run(t, dir, "rev-parse", "HEAD")
	lines, err := Blob(dir, head, "long.txt")
	if err != nil || len(lines) != 40 || lines[9] != "line ten" {
		t.Errorf("blob = %d lines, %v", len(lines), err)
	}
	if _, err := Blob(dir, head, "logo.bin"); err != ErrBinary {
		t.Errorf("binary: %v", err)
	}
	if _, err := Blob(dir, head, "gone.txt"); err == nil {
		t.Error("a deleted file has no blob at head")
	}
}
