package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

// deckWorkspace builds a workspace on disk with plain directories standing in
// for capsules (git status on them is simply empty).
func deckWorkspace(t *testing.T, capsules map[string][]string) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	ws := &workspace.Workspace{
		Root:         root,
		Org:          "acme",
		DisplayNames: map[string]string{"frontend": "Frontend", "backend": "API"},
	}
	for repo, names := range capsules {
		ws.RepoNames = append(ws.RepoNames, repo)
		os.MkdirAll(filepath.Join(ws.RepoDir(repo), workspace.GroundDir), 0o755)
		for _, n := range names {
			os.MkdirAll(filepath.Join(ws.RepoDir(repo), n), 0o755)
		}
	}
	return ws
}

func utcLocal(t *testing.T) {
	t.Helper()
	t.Setenv(harness.EnvDefault, "")
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
}

// loadedDeck returns a sized model with data loaded from ws.
func loadedDeck(t *testing.T, ws *workspace.Workspace, prs prIndex) deckModel {
	t.Helper()
	m := newDeckModel(ws, nil, "")
	m.prs = prs
	// As deck.go does after the program ends: stop crew this deck's
	// in-process host still runs, and wait for their ends to be recorded
	// (before the test's directories go).
	host := m.host
	t.Cleanup(host.Leave)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
	data, err := loadDeckData(ws, prs)
	if err != nil {
		t.Fatal(err)
	}
	next, _ = next.Update(deckLoadedMsg{data: data})
	return next.(deckModel)
}

func press(t *testing.T, m deckModel, keys ...string) (deckModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(deckModel)
	}
	return m, cmd
}

// runCmd executes cmd and feeds a resulting reload back through the model,
// reloading data from disk as the real program would.
func runCmd(t *testing.T, m deckModel, cmd tea.Cmd) deckModel {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	next, _ := m.Update(msg)
	m = next.(deckModel)
	data, err := loadDeckData(m.ws, m.prs)
	if err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(deckLoadedMsg{data: data})
	return next.(deckModel)
}

func seedMission(t *testing.T, ws *workspace.Workspace, title, brief string, capsules ...[2]string) string {
	t.Helper()
	store := mission.Store{Root: ws.Root}
	m, err := store.Create(title)
	if err != nil {
		t.Fatal(err)
	}
	if brief != "" {
		os.WriteFile(store.BriefPath(m.Slug), []byte(brief), 0o644)
	}
	for _, c := range capsules {
		if err := attachCapsule(ws, m.Slug, c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	return m.Slug
}

func TestLoadDeckData_SplitsOwnedAndLoose(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"auth-fix", "spare"}, "backend": {"auth-fix"}})
	seedMission(t, ws, "Auth fix", "", [2]string{"frontend", "auth-fix"}, [2]string{"backend", "auth-fix"})
	seedMission(t, ws, "Later", "")

	d, err := loadDeckData(ws, prIndex{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Missions) != 2 || d.Missions[0].M.Slug != "auth-fix" || d.Missions[0].State != mission.Active {
		t.Fatalf("missions = %+v", d.Missions)
	}
	if d.Missions[1].State != mission.Draft {
		t.Errorf("second mission state = %s", d.Missions[1].State)
	}
	if len(d.Loose) != 1 || d.Loose[0].Repo != "frontend" || d.Loose[0].Name != "spare" {
		t.Errorf("loose = %+v", d.Loose)
	}
}

func TestLoadDeckData_OrdersByState(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"live"}})
	seedMission(t, ws, "Draft first", "")
	seedMission(t, ws, "Live second", "", [2]string{"frontend", "live"})
	parked := seedMission(t, ws, "Parked third", "")
	setMissionState(ws, parked, mission.Parked)

	d, _ := loadDeckData(ws, prIndex{})
	var got []string
	for _, dm := range d.Missions {
		got = append(got, string(dm.State))
	}
	if strings.Join(got, ",") != "active,draft,parked" {
		t.Errorf("order = %v", got)
	}
}

func TestDeck_NavigationAndLooseToggle(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"a", "b", "c"}})
	seedMission(t, ws, "One", "", [2]string{"frontend", "a"})
	m := loadedDeck(t, ws, prIndex{})

	// rows: mission, capsule, loose header
	if len(m.rows) != 3 {
		t.Fatalf("rows = %d", len(m.rows))
	}
	m, _ = press(t, m, "j", "j")
	if r, _ := m.selected(); r.kind != deckRowLooseHeader {
		t.Fatalf("cursor on %v", r.kind)
	}
	m, _ = press(t, m, "enter")
	if len(m.rows) != 5 || !m.looseOpen {
		t.Fatalf("loose not expanded: %d rows", len(m.rows))
	}
	m, _ = press(t, m, "j", "j", "j") // clamps at the end
	if m.cursor != 4 {
		t.Errorf("cursor = %d, want 4", m.cursor)
	}
	m, _ = press(t, m, "g")
	if m.cursor != 0 {
		t.Errorf("g: cursor = %d", m.cursor)
	}
}

func TestDeck_CursorSurvivesReload(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"x"}})
	seedMission(t, ws, "Alpha", "")
	seedMission(t, ws, "Beta", "")
	m := loadedDeck(t, ws, prIndex{})
	m, _ = press(t, m, "j")
	before, _ := m.selectedMission()

	// A new mission sorts before them once it has a live capsule.
	seedMission(t, ws, "Gamma", "", [2]string{"frontend", "x"})
	data, _ := loadDeckData(ws, prIndex{})
	next, _ := m.Update(deckLoadedMsg{data: data})
	m = next.(deckModel)

	after, ok := m.selectedMission()
	if !ok || after.M.Slug != before.M.Slug {
		t.Errorf("cursor moved from %s to %v", before.M.Slug, after)
	}
}

func TestDeck_NewMission(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	m := loadedDeck(t, ws, prIndex{})

	m, cmd := press(t, m, "N")
	if m.mode != deckModeNewTitle || cmd == nil {
		t.Fatalf("mode = %v", m.mode)
	}
	m, _ = press(t, m, "S", "h", "i", "p")
	m, cmd = press(t, m, "enter")
	if m.mode != deckModeNormal || cmd == nil {
		t.Fatalf("after enter: mode %v, cmd %v", m.mode, cmd)
	}
	if _, err := (mission.Store{Root: ws.Root}).Load("ship"); err != nil {
		t.Fatalf("mission not created: %v", err)
	}
	// The command opens $EDITOR; skip it and reload as its callback would.
	next, _ := m.Update(deckReloadMsg{flash: "created ship"})
	m = next.(deckModel)
	data, _ := loadDeckData(ws, prIndex{})
	next, _ = m.Update(deckLoadedMsg{data: data})
	m = next.(deckModel)
	if dm, ok := m.selectedMission(); !ok || dm.M.Slug != "ship" {
		t.Errorf("new mission not selected")
	}
}

func TestDeck_NewMissionEscCancels(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	m := loadedDeck(t, ws, prIndex{})
	m, _ = press(t, m, "N", "x", "esc")
	if m.mode != deckModeNormal {
		t.Errorf("mode = %v", m.mode)
	}
	if list, _ := (mission.Store{Root: ws.Root}).List(); len(list) != 0 {
		t.Errorf("mission created on esc")
	}
}

func TestDeck_AttachLooseToSelectedMission(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"spare", "other"}})
	slug := seedMission(t, ws, "Target", "")
	m := selectMission(t, loadedDeck(t, ws, prIndex{}), slug)

	m, _ = press(t, m, "a")
	if m.mode != deckModePick || len(m.pick.items) != 2 {
		t.Fatalf("picker = %+v", m.pick)
	}
	m, _ = press(t, m, "j")
	m, cmd := press(t, m, "enter")
	m = runCmd(t, m, cmd)

	got, _ := mission.Store{Root: ws.Root}.Load(slug)
	if !got.HasCapsule("frontend", "spare") {
		t.Errorf("capsules = %+v", got.Capsules)
	}
	if !strings.Contains(m.flash, "frontend/spare → target") {
		t.Errorf("flash = %q", m.flash)
	}
}

func TestDeck_AttachFromLooseRowPicksMission(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"spare"}})
	seedMission(t, ws, "Alpha", "")
	beta := seedMission(t, ws, "Beta", "")
	m := loadedDeck(t, ws, prIndex{})

	m, _ = press(t, m, "G", "enter", "j") // loose capsules (last), expand, the capsule
	if r, _ := m.selected(); r.kind != deckRowLoose {
		t.Fatalf("not on loose capsule: %v", r.kind)
	}
	m, _ = press(t, m, "a", "j")
	_, cmd := press(t, m, "enter")
	runCmd(t, m, cmd)

	got, _ := mission.Store{Root: ws.Root}.Load(beta)
	if !got.HasCapsule("frontend", "spare") {
		t.Errorf("capsule not attached to beta: %+v", got.Capsules)
	}
}

func TestDeck_DetachPicksACapsule(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"x", "y"}})
	slug := seedMission(t, ws, "Det", "", [2]string{"frontend", "x"}, [2]string{"frontend", "y"})
	m := selectMission(t, loadedDeck(t, ws, prIndex{}), slug)

	m, _ = press(t, m, "d")
	if m.mode != deckModePick || len(m.pick.items) != 2 || m.pick.items[1] != "frontend/y" {
		t.Fatalf("picker = %+v", m.pick)
	}
	m, cmd := press(t, m, "esc")
	if cmd != nil || m.mode != deckModeNormal {
		t.Fatal("esc should cancel")
	}

	m, _ = press(t, m, "d", "j")
	_, cmd = press(t, m, "enter")
	runCmd(t, m, cmd)
	got, _ := (mission.Store{Root: ws.Root}).Load(slug)
	if got.HasCapsule("frontend", "y") || !got.HasCapsule("frontend", "x") {
		t.Errorf("capsules = %+v", got.Capsules)
	}
}

func TestDeck_ParkArchiveToggle(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	slug := seedMission(t, ws, "Toggle", "")
	m := selectMission(t, loadedDeck(t, ws, prIndex{}), slug)
	state := func() mission.State {
		got, _ := mission.Store{Root: ws.Root}.Load(slug)
		return got.State
	}

	_, cmd := press(t, m, "p")
	m = runCmd(t, m, cmd)
	if state() != mission.Parked {
		t.Fatalf("state = %q", state())
	}
	_, cmd = press(t, m, "p")
	m = runCmd(t, m, cmd)
	if state() != "" {
		t.Fatalf("p didn't reopen: %q", state())
	}

	m, _ = press(t, m, "x")
	m, cmd = press(t, m, "y")
	m = runCmd(t, m, cmd)
	if state() != mission.Archived {
		t.Fatalf("state = %q", state())
	}
	// Archived missions are behind the Past entry: overview, Past.
	if len(m.rows) != 2 || m.rows[1].kind != deckRowPastHeader {
		t.Errorf("archived mission still listed: %d rows", len(m.rows))
	}
	m, _ = press(t, m, "G", "enter")
	if len(m.rows) != 3 || m.rows[2].kind != deckRowMission {
		t.Errorf("⏎ on Past didn't list it: %d rows", len(m.rows))
	}
}

func TestDeck_SidebarClickSelectsRow(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"a"}})
	seedMission(t, ws, "One", "", [2]string{"frontend", "a"})
	seedMission(t, ws, "Two", "")
	m := loadedDeck(t, ws, prIndex{})

	_, rowOf := m.sidebarLines(deckSidebarWidth)
	target := -1
	for line, r := range rowOf {
		if r >= 0 && m.rows[r].kind == deckRowMission && m.data.Missions[m.rows[r].mission].M.Slug == "two" {
			target = line
		}
	}
	next, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainWidth() + 5, Y: target + 2})
	m = next.(deckModel)
	if dm, ok := m.selectedMission(); !ok || dm.M.Slug != "two" {
		t.Errorf("click selected %+v", dm)
	}
}

func TestDeck_SidebarHidesWhenNarrow(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	m := loadedDeck(t, ws, prIndex{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if next.(deckModel).sidebarShown() {
		t.Error("sidebar shown at 60 columns")
	}
}

// --- golden views ---

func goldenDeck(t *testing.T) deckModel {
	t.Helper()
	utcLocal(t)
	ws := deckWorkspace(t, map[string][]string{
		"frontend": {"token-race", "feat-x"},
		"backend":  {"token-race"},
	})
	slug := seedMission(t, ws, "Fix token refresh race",
		"# Fix token refresh race\n\n## Goal\n\nStop concurrent refreshes clobbering each other.\n\n- keep the public API\n- add a regression test\n",
		[2]string{"frontend", "token-race"}, [2]string{"backend", "token-race"})
	billing := seedMission(t, ws, "Billing export", "")
	// Pin times so the log renders the same every run.
	rewriteLogTimes(t, ws, slug)
	rewriteLogTimes(t, ws, billing)
	oldNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = oldNow })
	prs := prIndex{"backend": {open: map[string]*github.PR{"token-race": {Number: 88, HeadRefName: "token-race"}}}}
	m := loadedDeck(t, ws, prs)
	// Paths are temp dirs; show them relative to a fixed root.
	for i := range m.data.Missions {
		for j := range m.data.Missions[i].Capsules {
			c := &m.data.Missions[i].Capsules[j]
			c.Path = "/ws/repos/" + c.Repo + "/" + c.Name
		}
	}
	for i := range m.data.Loose {
		m.data.Loose[i].Path = "/ws/repos/" + m.data.Loose[i].Repo + "/" + m.data.Loose[i].Name
	}
	return m
}

func rewriteLogTimes(t *testing.T, ws *workspace.Workspace, slug string) {
	t.Helper()
	store := mission.Store{Root: ws.Root}
	events, _ := store.ReadLog(slug, 0)
	path := filepath.Join(store.Dir(), slug, "log.jsonl")
	os.Remove(path)
	at := time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)
	for i, e := range events {
		e.Time = at.Add(time.Duration(i) * time.Minute)
		store.AppendLog(slug, e)
	}
}

func TestGolden_Deck_Overview(t *testing.T) {
	m := goldenDeck(t) // the deck opens on the overview
	golden.RequireEqual(t, plainView(m))
}

func TestGolden_Deck_Mission(t *testing.T) {
	m := selectMission(t, goldenDeck(t), "fix-token-refresh-race")
	golden.RequireEqual(t, plainView(m))
}

func TestGolden_Deck_LooseCapsule(t *testing.T) {
	m := goldenDeck(t)
	m, _ = press(t, m, "G", "enter", "j")
	golden.RequireEqual(t, plainView(m))
}

func TestGolden_Deck_AttachPicker(t *testing.T) {
	m := selectMission(t, goldenDeck(t), "fix-token-refresh-race")
	m, _ = press(t, m, "a")
	golden.RequireEqual(t, plainView(m))
}

func TestGolden_Deck_Empty(t *testing.T) {
	utcLocal(t)
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	m := loadedDeck(t, ws, prIndex{})
	golden.RequireEqual(t, plainView(m))
}

func TestGolden_Deck_Help(t *testing.T) {
	m := goldenDeck(t)
	m, _ = press(t, m, "?")
	golden.RequireEqual(t, plainView(m))
}

func TestDeck_ArtifactsAndConclusion(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"cache-ui"}, "backend": {"cache-api"}})
	slug := seedMission(t, ws, "Cache", "## Goal\nCache it.", [2]string{"frontend", "cache-ui"}, [2]string{"backend", "cache-api"})
	store := mission.Store{Root: ws.Root}
	store.AddArtifact(slug, "https://example.com/design", "Design doc", "c1")

	open := prIndex{
		"frontend": {open: map[string]*github.PR{"cache-ui": {Number: 12, HeadRefName: "cache-ui", Title: "Cache UI"}}},
		"backend":  {merged: map[string]*github.PR{"cache-api": {Number: 88, HeadRefName: "cache-api", Title: "Cache API"}}},
	}
	m := selectMission(t, loadedDeck(t, ws, open), slug)
	_, content := m.mainContent(m.mainWidth()) // the whole pane, not just what fits on screen
	view := ansi.Strip(content)
	for _, want := range []string{"Artifacts", "frontend/cache-ui  #12 open  Cache UI", "backend/cache-api  #88 merged  Cache API", "Design doc  https://example.com/design"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Concluded") {
		t.Error("concluded with a PR still open")
	}

	merged := prIndex{
		"frontend": {merged: map[string]*github.PR{"cache-ui": {Number: 12, HeadRefName: "cache-ui"}}},
		"backend":  {merged: map[string]*github.PR{"cache-api": {Number: 88, HeadRefName: "cache-api"}}},
	}
	m = loadedDeck(t, ws, merged)
	m.pastOpen = true // landed: it's in the past
	m.rebuildRows()
	m = selectMission(t, m, slug)
	_, content = m.mainContent(m.mainWidth())
	if view := ansi.Strip(content); !strings.Contains(view, "✓ Concluded · every PR merged · x archives the mission") {
		t.Errorf("no conclusion hint:\n%s", view)
	}
}
