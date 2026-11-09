package review

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brudil/workspace/internal/mission"
)

// reviewing is a mission "cache" with a capsule changed since origin/main.
func reviewing(t *testing.T) (Store, Diffed) {
	t.Helper()
	dir := capsule(t)
	root := t.TempDir()
	ms := mission.Store{Root: root}
	if _, err := ms.Create("Cache"); err != nil {
		t.Fatal(err)
	}
	d, err := Compare(dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	return For(root, "cache"), Diffed{Dir: dir, BaseRef: "origin/main", Base: d.Base, Head: d.Head}
}

var at = time.Date(2026, 11, 9, 10, 12, 0, 0, time.UTC)

func ptr(s string) *string { return &s }

func TestDraftAndNotes(t *testing.T) {
	s, d := reviewing(t)
	r, err := s.AddNote("api/fix", d, NewNote{Path: "cache.go", Side: "new", Line: 3, EndLine: 4, Kind: "blocking", Body: "Key by tenant too."}, at)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "r1" || r.State != Draft || r.Head != d.Head || len(r.Comments) != 1 {
		t.Fatalf("draft = %+v", r)
	}
	c := r.Comments[0]
	if c.ID != "r1.c1" || c.Commit != d.Head || strings.Join(c.Excerpt, "|") != "func key(tenant, id string) string {|\treturn tenant + \"/\" + id" || c.Status != "open" {
		t.Errorf("comment = %+v", c)
	}
	// The old side's lines come from the base.
	r, err = s.AddNote("api/fix", d, NewNote{Path: "cache.go", Side: "old", Line: 3, Kind: "nit", Suggestion: ptr("")}, at)
	if err != nil || len(r.Comments) != 2 || r.Comments[1].ID != "r1.c2" || r.Comments[1].Commit != d.Base || r.Comments[1].Excerpt[0] != "func key(id string) string {" {
		t.Fatalf("old side = %+v, %v", r.Comments, err)
	}

	r, err = s.EditNote("api/fix", "r1.c1", "question", "Should the key include the tenant?", nil)
	if err != nil || r.Comments[0].Kind != "question" || r.Comments[0].Body != "Should the key include the tenant?" {
		t.Errorf("edit = %+v, %v", r.Comments[0], err)
	}
	r, err = s.DeleteNote("api/fix", "r1.c2")
	if err != nil || len(r.Comments) != 1 {
		t.Errorf("delete = %+v, %v", r.Comments, err)
	}
	// A new comment after a deletion doesn't reuse an id still in use.
	r, _ = s.AddNote("api/fix", d, NewNote{Path: "added.go", Side: "new", Line: 1, Kind: "nit", Body: "Doc comment?"}, at)
	if r.Comments[1].ID != "r1.c2" {
		t.Errorf("ids = %s, %s", r.Comments[0].ID, r.Comments[1].ID)
	}

	// Another capsule's draft is its own; drafts aren't anyone's latest.
	other, _ := s.AddNote("web/x", d, NewNote{Path: "cache.go", Side: "new", Line: 1, Kind: "nit", Body: "x"}, at)
	if other.ID != "r2" {
		t.Errorf("other draft = %s", other.ID)
	}
	if l, _ := s.Latest(""); l != nil {
		t.Errorf("a draft counted as submitted: %s", l.ID)
	}
	if err := s.Discard("web/x"); err != nil {
		t.Fatal(err)
	}
	if dr, _ := s.Draft("web/x"); dr != nil {
		t.Error("discarded draft is still there")
	}
}

func TestNoteValidation(t *testing.T) {
	s, d := reviewing(t)
	for name, n := range map[string]NewNote{
		"side":       {Path: "cache.go", Side: "left", Line: 1, Kind: "nit", Body: "x"},
		"kind":       {Path: "cache.go", Side: "new", Line: 1, Kind: "praise", Body: "x"},
		"empty":      {Path: "cache.go", Side: "new", Line: 1, Kind: "nit", Body: "  "},
		"past end":   {Path: "cache.go", Side: "new", Line: 5, EndLine: 9, Kind: "nit", Body: "x"},
		"backwards":  {Path: "cache.go", Side: "new", Line: 3, EndLine: 2, Kind: "nit", Body: "x"},
		"no file":    {Path: "nope.go", Side: "new", Line: 1, Kind: "nit", Body: "x"},
		"gone (new)": {Path: "gone.txt", Side: "new", Line: 1, Kind: "nit", Body: "x"},
	} {
		if _, err := s.AddNote("api/fix", d, n, at); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := s.EditNote("api/fix", "r1.c1", "nit", "x", nil); err == nil {
		t.Error("editing with no draft should fail")
	}
}

func TestSubmit(t *testing.T) {
	s, d := reviewing(t)
	if _, err := s.Submit("api/fix", d, Changes, "", at); err == nil {
		t.Error("changes requested with nothing said should fail")
	}
	if _, err := s.Submit("api/fix", d, "maybe", "x", at); err == nil {
		t.Error("unknown verdict should fail")
	}
	s.AddNote("api/fix", d, NewNote{Path: "cache.go", Side: "new", Line: 3, EndLine: 4, Kind: "blocking", Body: "Key by tenant.", Suggestion: ptr("func key(t Token) string {\n\treturn t.Tenant + \"/\" + t.ID")}, at)
	s.AddNote("api/fix", d, NewNote{Path: "added.go", Side: "new", Line: 3, Kind: "nit", Body: "Drop the dashes."}, at)
	r, err := s.Submit("api/fix", d, Changes, "Close.\nTwo things.", at)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != Submitted || r.Verdict != Changes || r.Submitted == nil || r.Summary != "Close.\nTwo things." {
		t.Errorf("submitted = %+v", r)
	}
	if l, _ := s.Latest("api/fix"); l == nil || l.ID != "r1" {
		t.Errorf("latest = %v", l)
	}

	md, err := os.ReadFile(s.renderedPath("r1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Review r1 of api/fix: changes requested",
		"> Close.\n> Two things.",
		"2 comments (1 blocking, 1 nit).",
		"## cache.go", "### r1.c1 · blocking · cache.go:3-4 at " + short(d.Head),
		"```go\n3  func key(tenant, id string) string {\n4  \treturn tenant + \"/\" + id\n```",
		"Suggested change, replacing these lines:\n\n```go\nfunc key(t Token) string {",
		// A line of backticks-like dashes is fine; a fence stays a fence.
		"## added.go", "### r1.c2 · nit · added.go:3 at",
		"## What to do", "report done with a handoff that says, for each comment by its id",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("rendered review lacks %q:\n%s", want, md)
		}
	}

	m, _ := s.Missions.Load("cache")
	if len(m.Artifacts) != 1 || m.Artifacts[0].Ref != "mission:reviews/r1.md" || m.Artifacts[0].Kind != "review" || m.Artifacts[0].Label != "Review r1 of api/fix: changes requested" {
		t.Errorf("artifacts = %+v", m.Artifacts)
	}
	log, _ := s.Missions.ReadLog("cache", 0)
	var reviews []string
	for _, e := range log {
		if e.Kind == mission.EventArtifact {
			t.Errorf("the artifact got a log line of its own: %+v", e)
		}
		if e.Kind == mission.EventReview {
			reviews = append(reviews, e.Repo+"/"+e.Capsule+" "+e.Text)
		}
	}
	if strings.Join(reviews, ";") != "api/fix r1: changes requested (1 blocking, 1 nit)" {
		t.Errorf("review log = %q", reviews)
	}

	// The next comment starts a new review; an approval needs no draft.
	r2, _ := s.AddNote("api/fix", d, NewNote{Path: "cache.go", Side: "new", Line: 1, Kind: "question", Body: "?"}, at)
	if r2.ID != "r2" {
		t.Errorf("next draft = %s", r2.ID)
	}
	s.Discard("api/fix")
	r3, err := s.Submit("api/fix", d, Approve, "", at.Add(time.Minute))
	if err != nil || r3.ID != "r2" || r3.Verdict != Approve || len(r3.Comments) != 0 {
		t.Errorf("approve = %+v, %v", r3, err)
	}
	if md := Render(r3); !strings.Contains(md, "No comments on the code.") || !strings.Contains(md, "Approved: the work is good to go.") {
		t.Errorf("approval:\n%s", md)
	}
}

func TestFence(t *testing.T) {
	var b strings.Builder
	fence(&b, "md", "```go\nx\n```\n")
	if !strings.HasPrefix(b.String(), "````md\n") || !strings.HasSuffix(b.String(), "````\n\n") {
		t.Errorf("fence = %q", b.String())
	}
}

func TestViewed(t *testing.T) {
	s, _ := reviewing(t)
	v, err := s.SetViewed("api/fix", "cache.go", "abc", true)
	if err != nil || v["cache.go"] != "abc" {
		t.Fatalf("viewed = %v, %v", v, err)
	}
	s.SetViewed("web/x", "a.ts", "def", true)
	v, _ = s.SetViewed("api/fix", "cache.go", "", false)
	if len(v) != 0 {
		t.Errorf("unviewed = %v", v)
	}
	if w, _ := s.Viewed("web/x"); w["a.ts"] != "def" {
		t.Errorf("other capsule = %v", w)
	}
	if w, _ := s.Viewed("none/here"); w == nil || len(w) != 0 {
		t.Errorf("none = %v", w)
	}
}
