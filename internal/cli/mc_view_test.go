package cli

import (
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
)

func viewMCModel() mcModel {
	ti := textinput.New()
	ti.Prompt = ""
	pi := textinput.New()
	pi.Prompt = ""

	return mcModel{
		ws: &workspace.Workspace{
			Org:          "testorg",
			Name:         "My Workspace",
			RepoNames:    []string{"repo1"},
			DisplayNames: map[string]string{},
			RepoColors:   map[string]string{},
			Boarded:      map[string][]string{},
		},
		rows: []mcRow{
			{kind: rowRepoHeader, repo: "repo1"},
			{kind: rowWorktree, repo: "repo1", wt: "main", branch: "main", loaded: true},
		},
		cursor:        1,
		width:         100,
		height:        40,
		detailFor:     -1,
		confirmIdx:    -1,
		actionSpinner: -1,
		filterInput:   ti,
		paletteInput:  pi,
	}
}

func TestRenderHeader_NoFilters(t *testing.T) {
	m := viewMCModel()
	header := m.renderHeader()

	if !strings.Contains(header, "My Workspace") {
		t.Error("header should contain workspace title")
	}
	if !strings.Contains(header, "/ to filter") {
		t.Error("header should contain '/ to filter' hint")
	}
}

func TestRenderHeader_WithFilters(t *testing.T) {
	m := viewMCModel()
	m.activeFilters = filterLocal | filterMine

	header := m.renderHeader()

	if !strings.Contains(header, "local") {
		t.Error("header should contain 'local' filter tag")
	}
	if !strings.Contains(header, "mine") {
		t.Error("header should contain 'mine' filter tag")
	}
}

func TestView_ZeroWidth(t *testing.T) {
	m := viewMCModel()
	m.width = 0

	got := m.View()
	if got != "loading..." {
		t.Errorf("View() = %q, want %q", got, "loading...")
	}
}

func TestRenderWorktreeRow_LiveIndicator(t *testing.T) {
	m := viewMCModel()
	row := mcRow{kind: rowWorktree, repo: "repo1", wt: "feat", loaded: true, live: true}

	got := m.renderWorktreeRow(row, 60)

	if !strings.Contains(got, "●") {
		t.Error("live row should contain ● indicator")
	}
}

func TestRenderDetail_LiveTag(t *testing.T) {
	m := viewMCModel()
	m.rows = append(m.rows, mcRow{kind: rowWorktree, repo: "repo1", wt: "feat", loaded: true, live: true})
	m.cursor = 2

	got := m.renderDetail(60)

	if !strings.Contains(got, "live") {
		t.Error("detail view should contain 'live' tag for live row")
	}
}

func TestView_ShowHelp(t *testing.T) {
	m := viewMCModel()
	m.showHelp = true

	got := m.View()
	if !strings.Contains(got, "Mission Control") {
		t.Error("help overlay should contain 'Mission Control'")
	}
	if !strings.Contains(got, "Navigate") {
		t.Error("help overlay should contain navigation help text")
	}
}
