package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/web"
	"github.com/brudil/workspace/internal/workspace"
)

// The web UI's mission page: what the deck's mission view shows, from the
// same helpers, so the two can't disagree.

// prCacheFor is how long a repo's PRs, once fetched, answer for the web UI:
// an open page refetches on every change, and GitHub is slow.
const prCacheFor = time.Minute

type prCache struct {
	mu  sync.Mutex
	at  map[string]time.Time
	prs prIndex
}

func newPRCache() *prCache { return &prCache{at: map[string]time.Time{}, prs: prIndex{}} }

// get is the repos' PRs, fetching those not fetched lately.
func (c *prCache) get(ws *workspace.Workspace, gh github.Client, repos []string) prIndex {
	c.mu.Lock()
	var stale []string
	for _, r := range repos {
		if time.Since(c.at[r]) > prCacheFor {
			stale = append(stale, r)
		}
	}
	c.mu.Unlock()
	fresh := fetchPRIndex(ws, gh, stale)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range stale {
		c.prs[r], c.at[r] = fresh[r], time.Now()
	}
	out := prIndex{}
	for _, r := range repos {
		out[r] = c.prs[r]
	}
	return out
}

// webMission is GET /api/missions/{slug}.
func webMission(root string, prs *prCache) func(slug string, withGitHub bool) (*web.MissionDetail, error) {
	return func(slug string, withGitHub bool) (*web.MissionDetail, error) {
		ctx, err := LoadContextFromDir(root)
		if err != nil {
			return nil, err
		}
		store := mission.Store{Root: root}
		m, err := store.Load(slug)
		if errors.Is(err, mission.ErrNotFound) {
			return nil, web.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		var idx prIndex
		if withGitHub && ctx.GitHub != nil {
			idx = prs.get(ctx.WS, ctx.GitHub, missionRepos([]*mission.Mission{m}))
		}
		v := buildMissionViewsFrom(ctx.WS, idx, []*mission.Mission{m})[0]
		brief, _ := store.ReadBrief(slug)
		events, _ := store.ReadLog(slug, 0)

		d := &web.MissionDetail{
			Slug: m.Slug, Title: m.Title, State: string(v.State), Created: m.Created, Request: m.Request, From: m.From,
			Brief: brief, GitHub: withGitHub,
			Stages: []web.Stage{}, Capsules: []web.CapsuleDetail{}, Crew: []web.CrewDetail{}, Artifacts: []web.Artifact{}, Events: []web.Event{},
		}
		if f := m.Flag; f != nil {
			d.Flag = &web.Proposal{Text: f.Text, By: f.By, Options: f.Options}
		}
		if dp := m.Dispatch; dp != nil {
			d.Dispatch = &web.Proposal{Text: dp.Text, By: dp.By, Capsule: dp.Capsule}
		}
		if c := m.Conclude; c != nil && v.State != mission.Archived {
			d.Conclude = &web.Proposal{Text: c.Text, By: c.By}
		}
		if states, _ := m.StageProgress(brief); !(len(states) == 1 && states[0].Implicit) {
			for _, st := range states {
				s := web.Stage{N: st.N, Title: oneLine(strings.ReplaceAll(st.Title(), "`", "")), State: "todo", CheckIn: st.CheckIn}
				if st.Record != nil {
					s.Crew, s.Why = st.Record.Crew, st.Record.Why
				}
				switch {
				case st.Accepted():
					s.State = "accepted"
				case st.Current:
					s.State = "current"
				}
				d.Stages = append(d.Stages, s)
			}
		}
		for _, c := range v.Capsules {
			cd := web.CapsuleDetail{Capsule: web.Capsule{Repo: c.Repo, Name: c.Name, Branch: c.Branch, Path: c.Path, Exists: c.Exists}}
			if c.Exists {
				st := workspace.QueryWorktreeStatus(c.Path)
				cd.Dirty, cd.Ahead, cd.Behind = st.Dirty, st.Ahead, st.Behind
				cd.Head = workspace.GitRevParse(c.Path, "HEAD")
			}
			if c.PR != nil {
				cd.PR = &web.PR{Number: c.PR.Number, Title: c.PR.Title, URL: c.PR.URL, Merged: c.PRS == mission.PRMerged, Checks: c.PR.StatusRollup}
			}
			d.Capsules = append(d.Capsules, cd)
		}
		for _, c := range m.Crew {
			cd := web.CrewDetail{
				Crew: web.Crew{ID: c.ID, Harness: c.Harness, Role: c.Role, Capsule: c.Capsule, Started: c.Started, Ended: c.Ended != nil,
					Activity: c.Activity, Status: c.Status, LastReply: c.LastReply},
				EndedAt: c.Ended, Exit: c.Exit, Note: c.Note, Dispatch: c.Dispatch,
			}
			if c.HandedOff != nil {
				cd.Handoff = mission.HandoffRef(c.ID)
			}
			d.Crew = append(d.Crew, cd)
		}
		for _, av := range resolveArtifacts(ctx.WS, store, v) {
			d.Artifacts = append(d.Artifacts, web.Artifact{
				Ref: av.Ref, Kind: av.Kind, Label: av.Label, State: av.State, URL: av.URL, Path: av.Path,
				By: av.Crew, Added: av.Added, Readable: av.State != "missing" && av.Path != "" && fileExists(av.Path),
			})
		}
		for _, e := range events {
			if text, ok := eventSentence(e); ok {
				d.Events = append(d.Events, web.Event{Time: e.Time, Kind: e.Kind, Text: text})
			}
		}
		return d, nil
	}
}

// artifactMax is the largest artifact the web UI shows.
const artifactMax = 2 << 20

// webArtifact is GET /api/missions/{slug}/artifact: a readable artifact's
// text, found by its reference among the mission's artifacts and resolved
// as the deck resolves it.
func webArtifact(root string) func(slug, ref string) (*web.ArtifactContent, error) {
	return func(slug, ref string) (*web.ArtifactContent, error) {
		ctx, err := LoadContextFromDir(root)
		if err != nil {
			return nil, err
		}
		store := mission.Store{Root: root}
		m, err := store.Load(slug)
		if errors.Is(err, mission.ErrNotFound) {
			return nil, web.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		v := buildMissionViewsFrom(ctx.WS, nil, []*mission.Mission{m})[0]
		for _, av := range resolveArtifacts(ctx.WS, store, v) {
			if av.Ref != ref {
				continue
			}
			a := web.Artifact{Ref: av.Ref, Kind: av.Kind, Label: av.Label, State: av.State, URL: av.URL, Path: av.Path, By: av.Crew, Added: av.Added}
			if av.Path == "" || av.State == "missing" {
				return nil, fmt.Errorf("%s isn't a file here to show", ref)
			}
			info, err := os.Stat(av.Path)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ref, err)
			}
			if info.Size() > artifactMax {
				return nil, fmt.Errorf("%s is too large to show (%d bytes)", ref, info.Size())
			}
			data, err := os.ReadFile(av.Path)
			if err != nil {
				return nil, err
			}
			if bytes.IndexByte(data, 0) >= 0 {
				return nil, fmt.Errorf("%s isn't text", ref)
			}
			a.Readable = true
			ext := strings.ToLower(filepath.Ext(av.Path))
			return &web.ArtifactContent{Artifact: a, Text: string(data), Markdown: ext == ".md" || ext == ".markdown" || ext == ".mdx"}, nil
		}
		return nil, fmt.Errorf("%w: mission %s has no artifact %s", web.ErrNotFound, slug, ref)
	}
}

// missionUpdated is when a mission's record, brief or log last changed: the
// web UI's pages hear of a change by it.
func missionUpdated(store mission.Store, slug string) time.Time {
	var latest time.Time
	for _, name := range []string{"mission.toml", "brief.md", "log.jsonl"} {
		if info, err := os.Stat(filepath.Join(store.Dir(), slug, name)); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}
