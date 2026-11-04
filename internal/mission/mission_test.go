package mission

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedNow(t *testing.T) {
	t.Helper()
	old := now
	now = func() time.Time { return time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = old })
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Fix token refresh race":  "fix-token-refresh-race",
		"  Hello,   World!  ":     "hello-world",
		"émoji 🚀 only":            "moji-only",
		"!!!":                     "mission",
		strings.Repeat("ab ", 30): "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateLoadList(t *testing.T) {
	fixedNow(t)
	s := Store{Root: t.TempDir()}

	a, err := s.Create("Auth fix")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("Auth fix")
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != "auth-fix" || b.Slug != "auth-fix-2" {
		t.Fatalf("slugs = %q, %q", a.Slug, b.Slug)
	}

	got, err := s.Load("auth-fix")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Auth fix" || !got.Created.Equal(now()) {
		t.Errorf("loaded %+v", got)
	}

	brief, _ := s.ReadBrief("auth-fix")
	if !strings.HasPrefix(brief, "# Auth fix\n") || !strings.Contains(brief, "## Acceptance") {
		t.Errorf("brief = %q", brief)
	}

	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].Slug != "auth-fix" {
		t.Fatalf("List = %v, %v", list, err)
	}

	events, _ := s.ReadLog("auth-fix", 0)
	if len(events) != 1 || events[0].Kind != EventCreated {
		t.Errorf("events = %+v", events)
	}
}

func TestCreate_RequiresTitle(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if _, err := s.Create("   "); err == nil {
		t.Error("want error for empty title")
	}
}

func TestLoad_NotFoundAndBadSlug(t *testing.T) {
	s := Store{Root: t.TempDir()}
	for _, slug := range []string{"nope", "../escape", ""} {
		if _, err := s.Load(slug); !errors.Is(err, ErrNotFound) {
			t.Errorf("Load(%q) err = %v, want ErrNotFound", slug, err)
		}
	}
}

func TestList_EmptyStore(t *testing.T) {
	s := Store{Root: t.TempDir()}
	list, err := s.List()
	if err != nil || len(list) != 0 {
		t.Errorf("List = %v, %v", list, err)
	}
}

func TestUpdate_ConcurrentWritersDontLoseCapsules(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, err := s.Create("Parallel")
	if err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Update(m.Slug, func(m *Mission) error {
				m.AddCapsule("repo", fmt.Sprintf("cap-%d", i))
				return nil
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	got, _ := s.Load(m.Slug)
	if len(got.Capsules) != n {
		t.Errorf("capsules = %d, want %d", len(got.Capsules), n)
	}
}

func TestUpdate_ErrorAbortsSave(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Abort")
	_, err := s.Update(m.Slug, func(m *Mission) error {
		m.AddCapsule("r", "c")
		return errors.New("nope")
	})
	if err == nil {
		t.Fatal("want error")
	}
	got, _ := s.Load(m.Slug)
	if len(got.Capsules) != 0 {
		t.Errorf("update was saved despite error")
	}
}

func TestUpdate_BreaksStaleLock(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Stale")
	lock := filepath.Join(s.missionDir(m.Slug), lockFile)
	os.WriteFile(lock, nil, 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(lock, old, old)

	if _, err := s.Update(m.Slug, func(*Mission) error { return nil }); err != nil {
		t.Fatalf("stale lock not broken: %v", err)
	}
}

func TestCapsuleMembership(t *testing.T) {
	m := &Mission{}
	if !m.AddCapsule("fe", "x") || m.AddCapsule("fe", "x") {
		t.Error("AddCapsule should add once")
	}
	if !m.HasCapsule("fe", "x") || m.HasCapsule("api", "x") {
		t.Error("HasCapsule wrong")
	}
	if !m.RemoveCapsule("fe", "x") || m.RemoveCapsule("fe", "x") {
		t.Error("RemoveCapsule should remove once")
	}
}

func TestFindByCapsule(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Owner")
	s.Update(m.Slug, func(m *Mission) error { m.AddCapsule("fe", "x"); return nil })

	if got, ok := s.FindByCapsule("fe", "x"); !ok || got.Slug != m.Slug {
		t.Errorf("FindByCapsule = %v, %v", got, ok)
	}
	if _, ok := s.FindByCapsule("fe", "y"); ok {
		t.Error("found a capsule no mission owns")
	}
}

func TestLog_AttributesCrewAndLimits(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Log")
	t.Setenv(EnvCrew, "c7")
	for i := range 5 {
		s.AppendLog(m.Slug, Event{Kind: EventNote, Text: fmt.Sprint(i)})
	}
	events, err := s.ReadLog(m.Slug, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Text != "3" || events[1].Text != "4" {
		t.Fatalf("events = %+v", events)
	}
	if events[1].Crew != "c7" {
		t.Errorf("crew = %q, want c7", events[1].Crew)
	}
}

func TestLog_SkipsMalformedLines(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Log")
	f, _ := os.OpenFile(s.logPath(m.Slug), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("not json\n")
	f.Close()
	s.AppendLog(m.Slug, Event{Kind: EventNote, Text: "after"})

	events, _ := s.ReadLog(m.Slug, 0)
	if len(events) != 2 || events[1].Text != "after" {
		t.Errorf("events = %+v", events)
	}
}

func TestDeriveState(t *testing.T) {
	type f = CapsuleFacts
	tests := []struct {
		name   string
		stored State
		facts  []f
		want   State
	}{
		{"no capsules", "", nil, Draft},
		{"only burned, no PR", "", []f{{Exists: false}}, Draft},
		{"live capsule", "", []f{{Exists: true}}, Active},
		{"some PRs", "", []f{{Exists: true, PR: PROpen}, {Exists: true}}, Active},
		{"all PRs", "", []f{{Exists: true, PR: PROpen}, {Exists: true, PR: PRMerged}}, Review},
		{"all merged", "", []f{{Exists: true, PR: PRMerged}, {Exists: false, PR: PRMerged}}, Landed},
		{"burned ignored", "", []f{{Exists: true, PR: PRMerged}, {Exists: false}}, Landed},
		{"parked wins", Parked, []f{{Exists: true, PR: PROpen}}, Parked},
		{"archived wins", Archived, nil, Archived},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Mission{State: tt.stored}
			for i := range tt.facts {
				m.AddCapsule("r", fmt.Sprint(i))
			}
			got := DeriveState(m, func(c Capsule) CapsuleFacts {
				var i int
				fmt.Sscan(c.Name, &i)
				return tt.facts[i]
			})
			if got != tt.want {
				t.Errorf("DeriveState = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCreate_ExcludesWsDirFromRootRepo(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if IsExcluded(root) {
		t.Fatal(".ws/ excluded before any mission exists")
	}
	s := Store{Root: root}
	if _, err := s.Create("One"); err != nil {
		t.Fatal(err)
	}
	if !IsExcluded(root) {
		t.Fatal(".ws/ not excluded after Create")
	}
	s.Create("Two")
	data, _ := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if n := strings.Count(string(data), excludeEntry); n != 1 {
		t.Errorf("exclude entry written %d times", n)
	}
	out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if strings.Contains(string(out), ".ws") {
		t.Errorf("git status shows .ws: %s", out)
	}
}

func TestIsExcluded_NonGitRoot(t *testing.T) {
	if !IsExcluded(t.TempDir()) {
		t.Error("non-git root should count as excluded")
	}
}

func TestCrewLifecycle(t *testing.T) {
	fixedNow(t)
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Crewed")

	c1, err := s.StartCrew(m.Slug, "cursor", "chat-1")
	if err != nil || c1.ID != "c1" {
		t.Fatalf("c1 = %+v, %v", c1, err)
	}
	c2, _ := s.StartCrew(m.Slug, "shell", "")
	if c2.ID != "c2" {
		t.Errorf("c2 id = %s", c2.ID)
	}
	if err := s.EndCrew(m.Slug, "c1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := s.EndCrew(m.Slug, "c9", "ok"); err == nil {
		t.Error("ending unknown crew should fail")
	}

	got, _ := s.Load(m.Slug)
	if got.Crew[0].Ended == nil || got.Crew[0].Exit != "ok" || got.Crew[1].Ended != nil {
		t.Errorf("crew = %+v", got.Crew)
	}
	if c, ok := got.LastResumable("cursor"); !ok || c.Conversation != "chat-1" {
		t.Errorf("LastResumable = %+v, %v", c, ok)
	}
	if _, ok := got.LastResumable("shell"); ok {
		t.Error("shell crew has no conversation")
	}

	events, _ := s.ReadLog(m.Slug, 0)
	kinds := []string{}
	for _, e := range events {
		kinds = append(kinds, e.Kind+":"+e.Crew)
	}
	if strings.Join(kinds, ",") != "created:,crew_started:c1,crew_started:c2,crew_ended:c1" {
		t.Errorf("log = %v", kinds)
	}
}

func TestReconcileCrew(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Reconcile")
	s.StartCrew(m.Slug, "shell", "") // c1: no pid, deck died before start
	s.StartCrew(m.Slug, "shell", "") // c2: pid alive
	s.StartCrew(m.Slug, "shell", "") // c3: pid dead
	s.StartCrew(m.Slug, "shell", "") // c4: already ended
	s.SetCrewProcess(m.Slug, "c2", 100, "")
	s.SetCrewProcess(m.Slug, "c3", 200, "")
	s.EndCrew(m.Slug, "c4", "ok")

	n := s.ReconcileCrew(func(pid int) bool { return pid == 100 })
	if n != 2 {
		t.Errorf("closed %d, want 2", n)
	}
	got, _ := s.Load(m.Slug)
	exits := []string{}
	for _, c := range got.Crew {
		exits = append(exits, c.Exit)
	}
	if strings.Join(exits, ",") != "lost,,lost,ok" {
		t.Errorf("exits = %v", exits)
	}
}

func TestCrewPIDOmittedWhenZero(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Pid")
	s.StartCrew(m.Slug, "shell", "")
	data, _ := os.ReadFile(filepath.Join(s.missionDir(m.Slug), missionFile))
	if strings.Contains(string(data), "pid") {
		t.Errorf("zero pid written:\n%s", data)
	}
}

func TestResumeCrew(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Resumable")
	s.StartCrew(m.Slug, "cursor", "chat-1")
	s.SetCrewProcess(m.Slug, "c1", 42, "fe/work")
	s.EndCrew(m.Slug, "c1", "stopped: hangup")
	s.StartCrew(m.Slug, "shell", "") // c2: nothing to resume

	got, _ := s.Load(m.Slug)
	c, ok := got.LastResumableCrew(func(string) bool { return false })
	if !ok || c.ID != "c1" || c.Capsule != "fe/work" {
		t.Fatalf("LastResumableCrew = %+v, %v", c, ok)
	}
	if _, ok := got.LastResumableCrew(func(id string) bool { return id == "c1" }); ok {
		t.Error("live crew offered for resume")
	}

	resumed, err := s.ResumeCrew(m.Slug, "c1")
	if err != nil || resumed.ID != "c1" || resumed.Conversation != "chat-1" {
		t.Fatalf("ResumeCrew = %+v, %v", resumed, err)
	}
	got, _ = s.Load(m.Slug)
	if len(got.Crew) != 2 || got.Crew[0].Ended != nil || got.Crew[0].Exit != "" || got.Crew[0].PID != 0 {
		t.Errorf("crew after resume = %+v", got.Crew)
	}
	events, _ := s.ReadLog(m.Slug, 1)
	if events[0].Kind != EventCrewResumed || events[0].Crew != "c1" {
		t.Errorf("last event = %+v", events[0])
	}
	if _, err := s.ResumeCrew(m.Slug, "c9"); err == nil {
		t.Error("resuming unknown crew should fail")
	}
}

func TestBriefingLifecycle(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, err := s.CreateBriefing("  fe issue 45\nplus more context  ")
	if err != nil {
		t.Fatal(err)
	}
	if m.Slug != "fe-issue-45" || m.Title != "fe issue 45" || m.State != Briefing || m.Request != "fe issue 45\nplus more context" {
		t.Fatalf("mission = %+v", m)
	}
	if got := DeriveState(m, func(Capsule) CapsuleFacts { return CapsuleFacts{Exists: true} }); got != Briefing {
		t.Errorf("briefing should win over derived state, got %s", got)
	}
	brief, _ := s.ReadBrief(m.Slug)
	if !IsUnwrittenBrief(brief, m.Title) {
		t.Errorf("new briefing brief = %q", brief)
	}
	if _, err := s.CreateBriefing("   "); err == nil {
		t.Error("empty request should fail")
	}

	s.WriteBrief(m.Slug, "## Goal\nFix the race")
	s.SetTitle(m.Slug, "Fix token refresh race")
	got, _ := s.Load(m.Slug)
	brief, _ = s.ReadBrief(m.Slug)
	if got.Title != "Fix token refresh race" || brief != "## Goal\nFix the race\n" || IsUnwrittenBrief(brief, got.Title) {
		t.Errorf("after edits: title %q brief %q", got.Title, brief)
	}

	if err := s.MarkReady(m.Slug); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReady(m.Slug); err == nil || !strings.Contains(err.Error(), "isn't being briefed") {
		t.Errorf("second ready err = %v", err)
	}

	if err := s.Rename(m.Slug, "token-race"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("fe-issue-45"); err == nil {
		t.Error("old slug still loads")
	}
	if got, err := s.Load("token-race"); err != nil || got.Title != "Fix token refresh race" {
		t.Errorf("renamed = %+v, %v", got, err)
	}
	s.Create("Other")
	if err := s.Rename("token-race", "other"); err == nil {
		t.Error("renaming onto an existing mission should fail")
	}
	if err := s.Rename("token-race", "Bad Slug"); err == nil {
		t.Error("invalid slug accepted")
	}
}

func TestIsUnwrittenBrief_Template(t *testing.T) {
	if !IsUnwrittenBrief(briefTemplate("X"), "X") || IsUnwrittenBrief("## Goal\nreal", "X") {
		t.Error("template detection wrong")
	}
}

func TestEndCrew_FirstEndWins(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Ends")
	s.StartCrewAs(m.Slug, "cursor", "", RoleBriefing)
	s.EndCrew(m.Slug, "c1", "briefed")
	s.EndCrew(m.Slug, "c1", "hangup")
	got, _ := s.Load(m.Slug)
	if got.Crew[0].Exit != "briefed" || got.Crew[0].Role != RoleBriefing {
		t.Errorf("crew = %+v", got.Crew[0])
	}
	events, _ := s.ReadLog(m.Slug, 0)
	ended := 0
	for _, e := range events {
		if e.Kind == EventCrewEnded {
			ended++
		}
	}
	if ended != 1 {
		t.Errorf("crew_ended logged %d times", ended)
	}
}

func TestBriefingCrewIsDoneOnceReady(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.CreateBriefing("api: cache it")
	s.StartCrewAs(m.Slug, "cursor", "conv-b", RoleBriefing)
	s.EndCrew(m.Slug, "c1", "hangup")
	never := func(string) bool { return false }

	got, _ := s.Load(m.Slug)
	if c, ok := got.LastResumableCrew(never); !ok || c.ID != "c1" {
		t.Fatalf("a stopped briefing should be resumable while briefing: %+v %v", c, ok)
	}
	s.MarkReady(m.Slug)
	got, _ = s.Load(m.Slug)
	if c, ok := got.LastResumableCrew(never); ok {
		t.Errorf("briefing crew offered for resume after ready: %+v", c)
	}
	if !got.Crew[0].Done(got) {
		t.Error("briefing crew not done after ready")
	}
	// Work crew after briefing are resumable as usual.
	s.StartCrew(m.Slug, "cursor", "conv-w")
	s.EndCrew(m.Slug, "c2", "hangup")
	got, _ = s.Load(m.Slug)
	if c, ok := got.LastResumableCrew(never); !ok || c.ID != "c2" {
		t.Errorf("work crew = %+v %v", c, ok)
	}
}

func TestCrewStatus(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Signals")
	s.StartCrew(m.Slug, "cursor", "conv")
	if err := s.SetCrewStatus(m.Slug, "c1", CrewAsking, "per-org or global?"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load(m.Slug)
	if c := got.Crew[0]; c.Status != CrewAsking || c.Note != "per-org or global?" || c.StatusAt == nil {
		t.Errorf("crew = %+v", c)
	}
	if cleared, _ := s.ClearCrewStatus(m.Slug, "c1"); !cleared {
		t.Error("nothing cleared")
	}
	if cleared, _ := s.ClearCrewStatus(m.Slug, "c1"); cleared {
		t.Error("cleared twice")
	}
	s.SetCrewStatus(m.Slug, "c1", CrewDone, "added the cache")
	s.EndCrew(m.Slug, "c1", "hangup")
	s.ResumeCrew(m.Slug, "c1")
	got, _ = s.Load(m.Slug)
	if got.Crew[0].Status != "" {
		t.Errorf("resume kept status %q", got.Crew[0].Status)
	}
	events, _ := s.ReadLog(m.Slug, 0)
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if j := strings.Join(kinds, ","); !strings.Contains(j, "crew_asked") || !strings.Contains(j, "crew_done") {
		t.Errorf("log = %s", j)
	}
	if err := s.SetCrewStatus(m.Slug, "c9", CrewDone, "x"); err == nil {
		t.Error("unknown crew accepted")
	}
}
