package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/deckserver"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/review"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/web"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newWebCmd() *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "web [path]",
		Short: "Open the deck's web UI in your browser",
		Long: `Open the workspace's web UI: missions now, review to come. It runs in the
deck server (started if it isn't running), on 127.0.0.1 at a port of its
own ([web] port in ws.toml or ws.local.toml picks another).

The link signs your browser in, so open it with this command (or o in
ws deck) rather than typing the address. A path opens that page, such as
/missions/<slug>.`,
		Args: cobra.MaximumNArgs(1),
		// Usage is for wrong arguments, not for what went wrong after.
		PreRun: func(cmd *cobra.Command, args []string) { cmd.SilenceUsage = true },
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			path := "/"
			if len(args) == 1 {
				path = "/" + strings.TrimPrefix(args[0], "/")
			}
			url, err := webURL(ctx.WS.Root, path)
			if err != nil {
				return err
			}
			if printOnly {
				fmt.Println(url)
				return nil
			}
			if err := openBrowser(url); err != nil {
				return fmt.Errorf("opening your browser: %w (ws web --print prints the link)", err)
			}
			info, _ := web.ReadInfo(ctx.WS.Root)
			fmt.Fprintf(ui.Stderr, "  %s opened %s%s\n", ui.Green.Render("✓"), info.Origin(), path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the sign-in link instead of opening it")
	return cmd
}

// webURL is a link that signs the browser in to the workspace's web UI and
// opens path, starting the deck server if it isn't running.
func webURL(root, path string) (string, error) {
	if !deckServerEnabled() {
		return "", fmt.Errorf("the web UI runs in the deck server, which %s=0 turns off", EnvDeckServer)
	}
	c, err := deckserver.Connect(root)
	if err != nil {
		return "", err
	}
	defer c.Close()
	if c.WebPort() == 0 {
		return "", noWebError(root, c)
	}
	// The server wrote web.json before it answered: it starts its web UI
	// first.
	info, err := web.ReadInfo(root)
	if err != nil {
		return "", err
	}
	if info.Pid != c.ServerPid() || info.Port != c.WebPort() {
		return "", fmt.Errorf("%s doesn't match the running deck server (pid %d, web UI on port %d); `ws deck server stop` and try again", web.InfoPath(root), c.ServerPid(), c.WebPort())
	}
	return info.SignInURL(path), nil
}

// noWebError says why a running deck server has no web UI, and what
// restarting it costs.
func noWebError(root string, c *deckserver.Client) error {
	crews, _ := c.List()
	restart := "it holds no crew, so `ws deck server stop` and then `ws web` loses nothing"
	if n := len(crews); n > 0 {
		restart = fmt.Sprintf("`ws deck server stop` and then `ws web` restarts it, stopping its %d crew (R in the deck resumes them)", n)
	}
	return fmt.Errorf("the deck server running for this workspace (pid %d) has no web UI: it was started by a ws from before there was one, or its web UI couldn't start (see %s); %s",
		c.ServerPid(), deckserver.LogPath(root), restart)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}

// startWeb runs the web UI inside the deck server. A web UI that can't
// start leaves the deck server running without one.
func startWeb(s *deckserver.Server) *web.Server {
	ctx, err := LoadContextFromDir(s.Root)
	port := 0
	if err == nil {
		port = ctx.Config.Web.Port
	}
	w := &web.Server{
		Root: s.Root, Port: port, Log: s.Log, Workspace: webWorkspace(s.Root), Capsule: webCapsule(s.Root), Hold: s.Hold,
		Mission: webMission(s.Root, newPRCache()), Artifact: webArtifact(s.Root),
		// A submitted review is the orchestrator's to act on.
		OnReview: func(slug string, r *review.Review) { orchestrateSpawner(s.Root, slug, "", "review") },
	}
	if err := w.Start(); err != nil {
		s.Log.Printf("no web UI: %v", err)
		return nil
	}
	return w
}

// webWorkspace is what the web UI's API serves: the workspace's missions
// with what git knows of their capsules (not GitHub: that's slow, and the
// deck fetches it).
func webWorkspace(root string) func() (web.Workspace, error) {
	return func() (web.Workspace, error) {
		ctx, err := LoadContextFromDir(root)
		if err != nil {
			return web.Workspace{}, err
		}
		missions, err := (mission.Store{Root: root}).List()
		if err != nil {
			return web.Workspace{}, err
		}
		out := web.Workspace{Name: ctx.WS.Name, Root: root, Missions: []web.Mission{}}
		if out.Name == "" {
			out.Name = filepath.Base(root)
		}
		for _, v := range buildMissionViewsFrom(ctx.WS, nil, missions) {
			m := web.Mission{Slug: v.M.Slug, Title: v.M.Title, State: string(v.State), Created: v.M.Created, Capsules: []web.Capsule{}, Crew: []web.Crew{},
				Updated: missionUpdated(mission.Store{Root: root}, v.M.Slug)}
			if v.M.Flag != nil {
				m.Flag = v.M.Flag.Text
			}
			for _, c := range v.Capsules {
				wc := web.Capsule{Repo: c.Repo, Name: c.Name, Branch: c.Branch, Path: c.Path, Exists: c.Exists}
				if c.Exists {
					wc.Head = workspace.GitRevParse(c.Path, "HEAD")
				}
				m.Capsules = append(m.Capsules, wc)
			}
			for _, c := range v.M.Crew {
				m.Crew = append(m.Crew, web.Crew{
					ID: c.ID, Harness: c.Harness, Role: c.Role, Capsule: c.Capsule, Started: c.Started,
					Ended: c.Ended != nil, Activity: c.Activity, Status: c.Status, LastReply: c.LastReply,
				})
			}
			out.Missions = append(out.Missions, m)
		}
		return out, nil
	}
}

// webCapsule finds a mission's capsule for review, and what it's compared
// with: origin's default branch, as the orchestrator's diffs are.
func webCapsule(root string) func(slug, repo, name string) (string, string, error) {
	return func(slug, repo, name string) (string, string, error) {
		ctx, err := LoadContextFromDir(root)
		if err != nil {
			return "", "", err
		}
		m, err := (mission.Store{Root: root}).Load(slug)
		if err != nil || !m.HasCapsule(repo, name) {
			return "", "", web.ErrNotFound
		}
		dir := filepath.Join(ctx.WS.RepoDir(repo), name)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return "", "", fmt.Errorf("capsule %s/%s is missing from %s", repo, name, dir)
		}
		branch := ctx.WS.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		return dir, "origin/" + branch, nil
	}
}

// reviewPath is where the web UI reviews a mission's capsule.
func reviewPath(slug, capsule string) string {
	return "/missions/" + slug + "/review/" + capsule
}

// review opens a capsule's review in the web UI: the selected crew's, or
// the selected mission's if it has only one; with more, its page, to pick.
func (m deckModel) review() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		m.flash = "select a mission to review"
		return m, nil
	}
	capsule := ""
	if r, ok := m.selectedRun(); ok && r.capsule != "" && dm.M.HasCapsule(splitCapsule(r.capsule)) {
		capsule = r.capsule
	} else if len(dm.M.Capsules) == 1 {
		capsule = dm.M.Capsules[0].Repo + "/" + dm.M.Capsules[0].Name
	}
	switch {
	case capsule != "":
		m.flash = "opening the review of " + capsule + "…"
		return m, openWebCmd(m.ws.Root, reviewPath(dm.M.Slug, capsule))
	case len(dm.M.Capsules) == 0:
		m.flash = "nothing to review: the mission has no capsules"
		return m, nil
	default:
		m.flash = "opening the mission: pick a capsule to review"
		return m, openWebCmd(m.ws.Root, "/missions/"+dm.M.Slug)
	}
}

func splitCapsule(c string) (repo, name string) {
	repo, name, _ = strings.Cut(c, "/")
	return repo, name
}

// openWebCmd opens path in the web UI from the deck, and says how it went.
func openWebCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		url, err := webURL(root, path)
		if err == nil {
			err = openBrowser(url)
		}
		if err != nil {
			return deckFlashMsg("web UI: " + err.Error())
		}
		info, _ := web.ReadInfo(root)
		return deckFlashMsg("opened " + info.Origin() + path + " in your browser")
	}
}
