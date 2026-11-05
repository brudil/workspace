package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
)

// Artifacts point at what a mission produced or relies on. A reference
// (mission.Artifact.Ref) is one of:
//
//	repo:path            a file on the repo's default branch (read from .ground)
//	repo/capsule:path    a file in one of the mission's capsules
//	mission:path         a file in the mission's own files/ folder
//	repo#N               a PR or issue
//	https://…            a link
//
// Where an artifact is now (draft, in review, landed, missing) is worked out
// each time from the capsules, their PRs and the files on disk.

type artifactLoc int

const (
	locLink artifactLoc = iota
	locGround
	locCapsule
	locMission
	locPR
)

type artifactRef struct {
	Loc     artifactLoc
	Repo    string
	Capsule string // the capsule's name, without its repo
	Path    string // slash-separated, relative
	Number  int
	URL     string
}

func (r artifactRef) String() string {
	switch r.Loc {
	case locGround:
		return r.Repo + ":" + r.Path
	case locCapsule:
		return r.Repo + "/" + r.Capsule + ":" + r.Path
	case locMission:
		return "mission:" + r.Path
	case locPR:
		return r.Repo + "#" + strconv.Itoa(r.Number)
	}
	return r.URL
}

func (r artifactRef) isFile() bool {
	return r.Loc == locGround || r.Loc == locCapsule || r.Loc == locMission
}

var (
	prRefShape = regexp.MustCompile(`^([A-Za-z0-9._-]+)#([0-9]+)$`)
	ghIssueURL = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/(?:pull|issues)/([0-9]+)/?(?:[?#].*)?$`)
	kindShape  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
)

// parseArtifactRef reads a stored reference. It trusts its form (it was
// normalised when added) and doesn't check that repos or files exist.
func parseArtifactRef(s string) artifactRef {
	switch {
	case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
		return artifactRef{Loc: locLink, URL: s}
	case strings.HasPrefix(s, "mission:"):
		return artifactRef{Loc: locMission, Path: strings.TrimPrefix(s, "mission:")}
	}
	if m := prRefShape.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[2])
		return artifactRef{Loc: locPR, Repo: m[1], Number: n}
	}
	if where, path, ok := strings.Cut(s, ":"); ok && path != "" {
		if repo, capsule, ok := strings.Cut(where, "/"); ok {
			return artifactRef{Loc: locCapsule, Repo: repo, Capsule: capsule, Path: path}
		}
		return artifactRef{Loc: locGround, Repo: where, Path: path}
	}
	return artifactRef{Loc: locLink, URL: s}
}

// defaultKind is the kind an artifact gets when none is given.
func defaultKind(r artifactRef) string {
	switch r.Loc {
	case locPR:
		return "pr"
	case locLink:
		return "link"
	}
	return "doc"
}

// normalizeArtifact turns what someone typed into a reference for mission
// m: a reference, a URL (a workspace repo's PR or issue URL becomes repo#N),
// or a path, classified by where it sits. Files must exist; a capsule must
// be one of the mission's. Errors say what to do instead.
func normalizeArtifact(ctx *Context, m *mission.Mission, input string) (artifactRef, error) {
	s := strings.TrimSpace(input)
	switch {
	case s == "":
		return artifactRef{}, errors.New("say what to record: a file, a reference like records:adr/0001.md or api#88, or a URL")
	case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
		if g := ghIssueURL.FindStringSubmatch(s); g != nil && strings.EqualFold(g[1], ctx.WS.Org) {
			if repo, ok := ctx.WS.ResolveAlias(g[2]); ok {
				n, _ := strconv.Atoi(g[3])
				return artifactRef{Loc: locPR, Repo: repo, Number: n}, nil
			}
		}
		return artifactRef{Loc: locLink, URL: s}, nil
	case strings.HasPrefix(s, "mission:"):
		rel, err := cleanArtifactPath(strings.TrimPrefix(s, "mission:"))
		if err != nil {
			return artifactRef{}, err
		}
		r := artifactRef{Loc: locMission, Path: rel}
		return r, artifactFileExists(ctx, m, r)
	}
	if g := prRefShape.FindStringSubmatch(s); g != nil {
		if repo, ok := ctx.WS.ResolveAlias(g[1]); ok {
			n, _ := strconv.Atoi(g[2])
			return artifactRef{Loc: locPR, Repo: repo, Number: n}, nil
		}
	}
	if where, path, ok := strings.Cut(s, ":"); ok {
		repoName, capsule, hasCapsule := strings.Cut(where, "/")
		if repo, ok := ctx.WS.ResolveAlias(repoName); ok {
			rel, err := cleanArtifactPath(path)
			if err != nil {
				return artifactRef{}, err
			}
			r := artifactRef{Loc: locGround, Repo: repo, Path: rel}
			if hasCapsule {
				r.Loc, r.Capsule = locCapsule, capsule
				if !m.HasCapsule(repo, capsule) {
					return artifactRef{}, fmt.Errorf("%s/%s isn't one of mission %s's capsules: attach it with ws mission attach %s %s", repo, capsule, m.Slug, repo, capsule)
				}
			}
			return r, artifactFileExists(ctx, m, r)
		}
	}
	return artifactFromPath(ctx, m, s)
}

// artifactFromPath classifies a file path by where it sits: one of the
// mission's capsules, a repo's .ground, or the mission's files folder.
func artifactFromPath(ctx *Context, m *mission.Mission, p string) (artifactRef, error) {
	if !filepath.IsAbs(p) {
		dir, err := userDir()
		if err != nil {
			return artifactRef{}, err
		}
		p = filepath.Join(dir, p)
	}
	if _, err := os.Stat(p); err != nil {
		return artifactRef{}, fmt.Errorf("no file %s: record a file that exists, a reference (repo:path, repo#N) or a URL", p)
	}
	abs := physicalPath(p)
	store := missionStore(ctx)
	if rel, ok := under(physicalPath(store.FilesDir(m.Slug)), abs); ok {
		return artifactRef{Loc: locMission, Path: filepath.ToSlash(rel)}, nil
	}
	rel, ok := under(physicalPath(ctx.WS.ReposDir()), abs)
	if ok {
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 3)
		if len(parts) == 3 && slices.Contains(ctx.WS.RepoNames, parts[0]) {
			repo, tree, file := parts[0], parts[1], parts[2]
			switch {
			case tree == workspace.GroundDir:
				return artifactRef{Loc: locGround, Repo: repo, Path: file}, nil
			case strings.HasPrefix(tree, "."):
				// .bare, .silo: not somewhere work is recorded from.
			case m.HasCapsule(repo, tree):
				return artifactRef{Loc: locCapsule, Repo: repo, Capsule: tree, Path: file}, nil
			default:
				return artifactRef{}, fmt.Errorf("%s/%s isn't one of mission %s's capsules: attach it with ws mission attach %s %s, or record the file once it's on the default branch (%s:%s)", repo, tree, m.Slug, repo, tree, repo, file)
			}
		}
	}
	return artifactRef{}, fmt.Errorf("%s isn't in one of the mission's capsules, a .ground, or the mission's files (%s): copy it into the mission's files to keep it privately, or record a URL", p, store.FilesDir(m.Slug))
}

// under reports whether p is inside dir, and its path relative to dir.
func under(dir, p string) (string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// cleanArtifactPath checks a path inside a reference stays inside where it
// points.
func cleanArtifactPath(p string) (string, error) {
	c := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "/")))
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%q isn't a path inside the repo or the mission's files", p)
	}
	return c, nil
}

// artifactFileExists checks that a file reference points at a file, saying
// where it looked if it doesn't.
func artifactFileExists(ctx *Context, m *mission.Mission, r artifactRef) error {
	var path string
	switch r.Loc {
	case locGround:
		path = filepath.Join(ctx.WS.MainWorktree(r.Repo), filepath.FromSlash(r.Path))
	case locCapsule:
		path = filepath.Join(ctx.WS.RepoDir(r.Repo), r.Capsule, filepath.FromSlash(r.Path))
	case locMission:
		path = filepath.Join(missionStore(ctx).FilesDir(m.Slug), filepath.FromSlash(r.Path))
	default:
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no file %s (for %s)", path, r)
	}
	return nil
}

// artifactView is an artifact with where it is now.
type artifactView struct {
	mission.Artifact
	ref artifactRef
	// State is where it is now: draft, in review, landed, missing (files);
	// private (mission files); open, merged, pr (PRs); link.
	State string
	Path  string // the local file to read, for files
	URL   string // where to open or share it, when there's one
}

// resolveArtifacts works out where each of the mission's artifacts is now,
// from its capsules (and their PRs) and the files on disk.
func resolveArtifacts(ws *workspace.Workspace, store mission.Store, v missionView) []artifactView {
	out := make([]artifactView, 0, len(v.M.Artifacts))
	for _, a := range v.M.Artifacts {
		out = append(out, resolveArtifact(ws, store, v, a))
	}
	return out
}

func resolveArtifact(ws *workspace.Workspace, store mission.Store, v missionView, a mission.Artifact) artifactView {
	r := parseArtifactRef(a.Ref)
	av := artifactView{Artifact: a, ref: r}
	if av.Kind == "" {
		av.Kind = defaultKind(r)
	}
	repoURL := "https://github.com/" + ws.Org + "/" + r.Repo
	onGround := func() (string, bool) {
		p := filepath.Join(ws.MainWorktree(r.Repo), filepath.FromSlash(r.Path))
		return p, fileExists(p)
	}
	landed := func(p string) {
		av.State, av.Path = "landed", p
		av.URL = repoURL + "/blob/" + ws.DefaultBranch + "/" + r.Path
	}
	switch r.Loc {
	case locLink:
		av.State, av.URL = "link", r.URL
	case locPR:
		av.State, av.URL = "pr", fmt.Sprintf("%s/pull/%d", repoURL, r.Number)
		if av.Kind == "issue" {
			av.URL = fmt.Sprintf("%s/issues/%d", repoURL, r.Number)
		}
		for _, c := range v.Capsules {
			if c.Repo == r.Repo && c.PR != nil && c.PR.Number == r.Number {
				av.State = map[bool]string{true: "merged", false: "open"}[c.PRS == mission.PRMerged]
			}
		}
	case locMission:
		av.Path = filepath.Join(store.FilesDir(v.M.Slug), filepath.FromSlash(r.Path))
		av.State = map[bool]string{true: "private", false: "missing"}[fileExists(av.Path)]
	case locGround:
		if p, ok := onGround(); ok {
			landed(p)
		} else {
			av.State, av.Path = "missing", p
		}
	case locCapsule:
		groundPath, groundHas := onGround()
		var capsule *missionCapsuleView
		for i := range v.Capsules {
			if c := &v.Capsules[i]; c.Repo == r.Repo && c.Name == r.Capsule && c.Exists {
				capsule = c
			}
		}
		if capsule == nil { // burned (or detached): it's on the default branch now, or nowhere
			if groundHas {
				landed(groundPath)
			} else {
				av.State = "missing"
			}
			break
		}
		capPath := filepath.Join(capsule.Path, filepath.FromSlash(r.Path))
		capHas := fileExists(capPath)
		switch {
		case capsule.PRS == mission.PRMerged && groundHas:
			landed(groundPath)
		case capsule.PRS == mission.PRMerged && capHas:
			landed(capPath) // .ground hasn't caught up yet
		case !capHas && groundHas:
			landed(groundPath)
		case !capHas:
			av.State = "missing"
		case capsule.PR != nil:
			av.State, av.Path, av.URL = "in review", capPath, capsule.PR.URL
		default:
			av.State, av.Path = "draft", capPath
		}
	}
	return av
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// artifactLine is one artifact, for agents: kind, state, reference, label,
// and where to read and open it.
func artifactLine(av artifactView) string {
	line := fmt.Sprintf("[%s, %s] %s", av.Kind, av.State, av.Ref)
	if av.Label != "" {
		line += " — " + av.Label
	}
	if av.Path != "" && av.State != "missing" {
		line += " — read: " + av.Path
	}
	if av.URL != "" && av.ref.Loc != locLink {
		line += " — " + av.URL
	}
	return line
}

// artifactStateStyle colours a state: landed and merged are done, drafts
// and reviews are in progress, missing is a problem.
func artifactStateStyle(state string) string {
	switch state {
	case "landed", "merged":
		return ui.Green.Render(state)
	case "in review", "open", "draft":
		return ui.Blue.Render(state)
	case "missing":
		return ui.Orange.Render(state)
	}
	return ui.Dim.Render(state)
}

// artifactText is one artifact for people: kind, state, label (or the
// reference), linked when there's a link, with the reference after.
func artifactText(av artifactView) string {
	name := av.Label
	if name == "" {
		name = av.Ref
	}
	if av.URL != "" {
		name = ui.Hyperlink(av.URL, name)
	}
	line := fmt.Sprintf("%s  %s  %s", ui.Dim.Render(av.Kind), artifactStateStyle(av.State), name)
	if av.Label != "" {
		line += ui.Dim.Render("  " + av.Ref)
	}
	return line
}
