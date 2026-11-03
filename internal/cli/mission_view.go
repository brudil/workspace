package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
)

func missionStore(ctx *Context) mission.Store {
	return mission.Store{Root: ctx.WS.Root}
}

// resolveMission picks the mission a command applies to: an explicit slug,
// then $WS_MISSION, then the mission owning the capsule the user stands in.
func resolveMission(ctx *Context, slug string) (*mission.Mission, error) {
	store := missionStore(ctx)
	if slug == "" {
		slug = os.Getenv(mission.EnvMission)
	}
	if slug != "" {
		m, err := store.Load(slug)
		if errors.Is(err, mission.ErrNotFound) {
			return nil, fmt.Errorf("no mission %q (see ws mission list)", slug)
		}
		return m, err
	}
	cwd, _ := userDir()
	if repo, capsule, ok := workspace.DetectRepo(ctx.WS.Root, cwd); ok {
		if m, ok := store.FindByCapsule(repo, capsule); ok {
			return m, nil
		}
	}
	return nil, errors.New("no mission given: pass a mission slug, set " + mission.EnvMission + ", or run from a mission's capsule")
}

// envMission returns the mission named by $WS_MISSION, or nil when it is
// unset. A set-but-missing mission is reported as an error so callers can warn.
func envMission(ctx *Context) (*mission.Mission, error) {
	slug := os.Getenv(mission.EnvMission)
	if slug == "" {
		return nil, nil
	}
	m, err := missionStore(ctx).Load(slug)
	if errors.Is(err, mission.ErrNotFound) {
		return nil, fmt.Errorf("%s=%s does not exist", mission.EnvMission, slug)
	}
	return m, err
}

// missionCapsuleView is a mission capsule with live facts attached.
type missionCapsuleView struct {
	mission.Capsule
	Path   string
	Exists bool
	Branch string
	PR     *github.PR
	PRS    mission.PRState
}

type missionView struct {
	M        *mission.Mission
	State    mission.State
	Capsules []missionCapsuleView
}

// buildMissionViews gathers git and (when gh is non-nil) GitHub facts for
// every capsule of the given missions, fetching each repo's PRs once.
func buildMissionViews(ws *workspace.Workspace, gh github.Client, missions []*mission.Mission) []missionView {
	repos := map[string]bool{}
	for _, m := range missions {
		for _, c := range m.Capsules {
			repos[c.Repo] = true
		}
	}

	type repoPRs struct{ open, merged map[string]*github.PR }
	prs := make(map[string]repoPRs, len(repos))
	if gh != nil {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for repo := range repos {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := repoPRs{open: indexPRs(gh.PRsForRepo(ws.Org, repo))}
				r.merged = indexPRs(gh.MergedPRsForRepo(ws.Org, repo))
				mu.Lock()
				prs[repo] = r
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	views := make([]missionView, len(missions))
	for i, m := range missions {
		v := missionView{M: m}
		facts := map[mission.Capsule]mission.CapsuleFacts{}
		for _, c := range m.Capsules {
			cv := missionCapsuleView{Capsule: c, Path: filepath.Join(ws.RepoDir(c.Repo), c.Name)}
			if info, err := os.Stat(cv.Path); err == nil && info.IsDir() {
				cv.Exists = true
				cv.Branch = workspace.GitCurrentBranch(cv.Path)
			}
			if cv.Branch == "" {
				cv.Branch = c.Branch
			}
			r := prs[c.Repo]
			if pr := lookupPR(r.open, cv.Branch, c.Name); pr != nil {
				cv.PR, cv.PRS = pr, mission.PROpen
			} else if pr := lookupPR(r.merged, cv.Branch, c.Name); pr != nil {
				cv.PR, cv.PRS = pr, mission.PRMerged
			}
			facts[c] = mission.CapsuleFacts{Exists: cv.Exists, PR: cv.PRS}
			v.Capsules = append(v.Capsules, cv)
		}
		v.State = mission.DeriveState(m, func(c mission.Capsule) mission.CapsuleFacts { return facts[c] })
		views[i] = v
	}
	return views
}

func indexPRs(list []github.PR, err error) map[string]*github.PR {
	if err != nil {
		return nil
	}
	m := make(map[string]*github.PR, len(list))
	for i := range list {
		m[list[i].HeadRefName] = &list[i]
	}
	return m
}

func gitBranchOf(ctx *Context, repo, capsule string) string {
	return workspace.GitCurrentBranch(filepath.Join(ctx.WS.RepoDir(repo), capsule))
}

// attachToEnvMission adds a newly created capsule to $WS_MISSION, if set.
// It never fails the command that created the capsule; problems are warnings.
func attachToEnvMission(ctx *Context, repo, capsule string) {
	m, err := envMission(ctx)
	if err != nil {
		fmt.Fprintf(ui.Stderr, "  %s %v; capsule not attached to a mission\n", ui.Orange.Render("⚠"), err)
		return
	}
	if m == nil {
		return
	}
	if err := attachCapsule(ctx, m.Slug, repo, capsule); err != nil {
		fmt.Fprintf(ui.Stderr, "  %s attaching to mission %s: %v\n", ui.Orange.Render("⚠"), m.Slug, err)
		return
	}
	fmt.Fprintf(ui.Stderr, "  %s mission %s\n", ui.Dim.Render("→"), m.Slug)
}

// rememberMissionBranch records the branch of a capsule about to be burned on
// the mission that owns it, so the mission can still find its PR afterwards.
// It returns the owning mission's slug, or "" if no mission owns it.
func rememberMissionBranch(ctx *Context, repo, capsule string) string {
	store := missionStore(ctx)
	owner, ok := store.FindByCapsule(repo, capsule)
	if !ok {
		return ""
	}
	if branch := gitBranchOf(ctx, repo, capsule); branch != "" {
		store.Update(owner.Slug, func(m *mission.Mission) error {
			m.AddCapsuleBranch(repo, capsule, branch)
			return nil
		})
	}
	return owner.Slug
}

// logMissionBurn records a burn on the owning mission. The capsule stays in
// the mission (shown as burned) so its PR still counts toward the state.
func logMissionBurn(ctx *Context, slug, repo, capsule string) {
	if slug == "" {
		return
	}
	missionStore(ctx).AppendLog(slug, mission.Event{Kind: mission.EventCapsuleBurned, Repo: repo, Capsule: capsule})
}

// missionLLMHeader is printed above `ws status -f llm` output when crew run it
// with $WS_MISSION set. It is empty otherwise, leaving status unchanged.
func missionLLMHeader(ws *workspace.Workspace, gh github.Client) string {
	slug := os.Getenv(mission.EnvMission)
	if slug == "" {
		return ""
	}
	m, err := mission.Store{Root: ws.Root}.Load(slug)
	if err != nil {
		return fmt.Sprintf("Mission: %s (not found — %s points at a mission that doesn't exist)\n\n", slug, mission.EnvMission)
	}
	v := buildMissionViews(ws, gh, []*mission.Mission{m})[0]
	var b strings.Builder
	fmt.Fprintf(&b, "Mission: %s — %s [%s]\n", m.Slug, m.Title, v.State)
	if len(v.Capsules) == 0 {
		b.WriteString("Mission capsules: none yet\n")
	} else {
		names := make([]string, len(v.Capsules))
		for i, c := range v.Capsules {
			names[i] = c.Repo + "/" + c.Name
			if !c.Exists {
				names[i] += " (burned)"
			}
		}
		fmt.Fprintf(&b, "Mission capsules: %s\n", strings.Join(names, ", "))
	}
	b.WriteString("You are crew on this mission. Run `ws mission show -f llm` for the brief.\n\n")
	return b.String()
}
