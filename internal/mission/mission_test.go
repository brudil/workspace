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
	s.SetCrewPID(m.Slug, "c2", 100)
	s.SetCrewPID(m.Slug, "c3", 200)
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
