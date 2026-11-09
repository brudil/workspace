import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import { type Artifact, type CapsuleDetail, type CrewDetail, getMission, type MissionDetail, type MissionState, type Workspace } from "../api";
import { useKeys } from "../keys";
import { Markdown } from "../markdown/LazyMarkdown";

// The mission page: the deck's mission view, in four tabs. Summary leads
// with what needs you; Brief is the brief; Work is the capsules and
// artifacts; Log is the log and the crew. A details sidebar beside them
// keeps what you glance at in view: state, stages, capsules, crew and
// artifacts.

const tabs = ["summary", "brief", "work", "log"] as const;
type Tab = (typeof tabs)[number];
const tabNames: Record<Tab, string> = { summary: "Summary", brief: "Brief", work: "Work", log: "Log" };

export const missionKeys: [string, string][] = [
  ["1–4", "Summary, Brief, Work, Log"],
  ["j / k", "Next / previous capsule, artifact or handoff (on Summary, the sidebar's)"],
  ["⏎", "Open it: a capsule's review, an artifact"],
  ["v", "Review the capsule (or the mission's only one)"],
  ["o", "Open its link (a PR, the artifact on GitHub)"],
  ["s", "Hide or show the details sidebar"],
  ["r", "Reload"],
  ["esc", "Back to the missions"],
];

const sideKey = "ws:mission-side";

// remembered is each mission's selection, kept while you open what's
// selected and come back.
const remembered = new Map<string, string>();

function loadSide(): boolean {
  try {
    return localStorage.getItem(sideKey) !== "0";
  } catch {
    return true;
  }
}

// Item is something on the page the cursor can be on.
interface Item {
  key: string;
  open: () => void;
  url?: string;
  capsule?: CapsuleDetail;
}

export function MissionPage({ workspace }: { workspace: Workspace }) {
  const { slug = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const tab: Tab = tabs.includes(params.get("tab") as Tab) ? (params.get("tab") as Tab) : "summary";
  const navigate = useNavigate();
  const updated = workspace.missions.find((m) => m.slug === slug)?.updated;
  const [side, setSide] = useState(loadSide);

  const [d, setD] = useState<MissionDetail>();
  const [error, setError] = useState<string>();
  // What's on disk first (fast), then again with the capsules' PRs (GitHub
  // is slow); again whenever the mission changes.
  const load = useCallback(() => {
    let live = true;
    getMission(slug, false)
      .then((x) => {
        if (!live) return;
        setD((cur) => (cur?.github ? { ...x, capsules: withPRs(x.capsules, cur.capsules), github: true } : x));
        setError(undefined);
        return getMission(slug, true).then((y) => live && setD(y));
      })
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, [slug]);
  useEffect(load, [load, updated]);

  const openArtifact = useCallback(
    (a: Artifact) => {
      if (a.readable) navigate(`/missions/${slug}/artifact?${new URLSearchParams({ ref: a.ref })}`);
      else if (a.url) window.open(a.url, "_blank", "noopener");
    },
    [navigate, slug],
  );
  const review = useCallback((c: CapsuleDetail) => c.exists && navigate(`/missions/${slug}/review/${c.repo}/${c.name}`), [navigate, slug]);

  const items = useMemo((): Item[] => {
    if (!d) return [];
    const caps = d.capsules.map((c): Item => ({ key: `c:${c.repo}/${c.name}`, open: () => review(c), url: c.pr?.url, capsule: c }));
    const arts = (list: Artifact[]) => list.map((a): Item => ({ key: `a:${a.ref}`, open: () => openArtifact(a), url: a.url }));
    switch (tab) {
      case "summary":
        // In the order the sidebar (or Summary without it) shows them.
        return [...arts(d.artifacts.slice(0, side ? sideArtifacts : summaryArtifacts)), ...caps];
      case "work":
        return [...caps, ...arts(byKind(d.artifacts).flatMap(([, list]) => list))];
      case "log":
        return d.crew
          .filter((c) => c.handoff)
          .map((c): Item => ({ key: `h:${c.id}`, open: () => openArtifact({ ref: c.handoff!, readable: true } as Artifact) }));
    }
    return [];
  }, [d, tab, side, review, openArtifact]);
  const [sel, setSelState] = useState<string | undefined>(() => remembered.get(slug));
  const setSel = (key: string) => {
    remembered.set(slug, key);
    setSelState(key);
  };
  // Nothing's selected until j or k: the first j takes the first item.
  const at = items.findIndex((i) => i.key === sel);
  const item = at >= 0 ? items[at] : undefined;
  useEffect(() => {
    if (!item) return;
    (document.getElementById(`item-${item.key}`) ?? document.getElementById(`side-${item.key}`))?.scrollIntoView({ block: "nearest" });
  }, [item?.key]);

  const move = (to: number) => items.length && setSel(items[Math.min(Math.max(to, 0), items.length - 1)].key);
  const setTab = (t: Tab) => setParams(t === "summary" ? {} : { tab: t }, { replace: true });
  const back = () => navigate("/");
  useKeys({
    "1": () => setTab("summary"),
    "2": () => setTab("brief"),
    "3": () => setTab("work"),
    "4": () => setTab("log"),
    j: () => move(at + 1),
    ArrowDown: () => move(at + 1),
    k: () => move(at < 0 ? 0 : at - 1),
    ArrowUp: () => move(at < 0 ? 0 : at - 1),
    Enter: () => item?.open(),
    o: () => (item?.url ? window.open(item.url, "_blank", "noopener") : item?.open()),
    v: () => {
      const only = d?.capsules.filter((c) => c.exists);
      const c = item?.capsule ?? (only?.length === 1 ? only[0] : undefined);
      if (c) review(c);
    },
    s: () =>
      setSide((v) => {
        try {
          localStorage.setItem(sideKey, v ? "0" : "1");
        } catch {
          // kept for this page only
        }
        return !v;
      }),
    r: load,
    Escape: back,
    h: back,
  });

  if (error && !d) {
    return (
      <div className="page">
        <h1>{error}</h1>
      </div>
    );
  }
  if (!d) return <div className="page dim">Loading…</div>;
  const selected = (key: string) => (item?.key === key ? " selected" : "");

  const parts = { d, selected, openArtifact, review };
  return (
    <div className={`page mission${side ? " with-side" : ""}`}>
      <div className="mission-main">
        <header className="page-head">
          <Link to="/" className="dim">
            Missions
          </Link>
          <span className="dim">/</span>
          <h1>{d.title}</h1>
          {!side && <StateLabel state={d.state} />}
        </header>
        <nav className="tabs">
          {tabs.map((t, i) => (
            <button key={t} className={t === tab ? "on" : ""} onClick={() => setTab(t)}>
              {tabNames[t]} <kbd>{i + 1}</kbd>
            </button>
          ))}
          {!side && (
            <button className="side-toggle" onClick={() => setSide(true)}>
              Details <kbd>s</kbd>
            </button>
          )}
        </nav>
        {error && <div className="banner">{error}</div>}
        {tab === "summary" && <Summary {...parts} full={!side} />}
        {tab === "brief" && <Brief d={d} />}
        {tab === "work" && <Work {...parts} />}
        {tab === "log" && <Log {...parts} />}
      </div>
      {side && <DetailSide {...parts} updated={updated} onHide={() => setSide(false)} />}
    </div>
  );
}

// DetailSide is the mission's details beside its tabs: what it is, where
// its stages are, its capsules, who's working and what it's made.
function DetailSide({ d, selected, openArtifact, review, updated, onHide }: Parts & { updated?: string; onHide: () => void }) {
  const live = d.crew.filter((c) => !c.ended);
  const accepted = d.stages.filter((s) => s.state === "accepted").length;
  return (
    <aside className="detail-side">
      <div className="side-head">
        <span className="dim">Details</span>
        <button className="link dim" onClick={onHide} title="Hide (s)">
          <kbd>s</kbd>
        </button>
      </div>
      <dl className="props">
        <dt>State</dt>
        <dd>
          <StateLabel state={d.state} />
        </dd>
        <dt>Created</dt>
        <dd>{stamp(d.created)}</dd>
        {updated && (
          <>
            <dt>Updated</dt>
            <dd>{ago(updated)}</dd>
          </>
        )}
        <dt>Slug</dt>
        <dd className="mono">{d.slug}</dd>
        {d.from && (
          <>
            <dt>Raised from</dt>
            <dd className="mono">{d.from}</dd>
          </>
        )}
      </dl>
      {d.stages.length > 0 && (
        <SideSection title="Stages" count={`${accepted}/${d.stages.length}`}>
          <Stages d={d} compact />
        </SideSection>
      )}
      {d.artifacts.length > 0 && (
        <SideSection title="Artifacts" count={d.artifacts.length}>
          {d.artifacts.slice(0, sideArtifacts).map((a) => {
            const key = `a:${a.ref}`;
            return (
              <div
                key={key}
                id={`side-${key}`}
                className={`item side-item${selected(key)}${a.readable || a.url ? " openable" : ""}`}
                onClick={() => openArtifact(a)}
                title={a.ref}
              >
                <div className="side-art">
                  <span className={`dot ${stateClass[a.state] ?? ""}`} title={a.state} />
                  <span className="name">{a.label || a.ref}</span>
                </div>
                <div className="dim small">
                  {a.kind} · {a.state}
                </div>
              </div>
            );
          })}
          {d.artifacts.length > sideArtifacts && <div className="dim">+{d.artifacts.length - sideArtifacts} more · 3 for the work</div>}
        </SideSection>
      )}
      <SideSection title="Capsules" count={d.capsules.length || undefined}>
        {d.capsules.map((c) => {
          const key = `c:${c.repo}/${c.name}`;
          return (
            <div key={key} id={`side-${key}`} className={`item side-item${selected(key)}`} onClick={() => review(c)} title={c.exists ? "Review it (v)" : "burned"}>
              <div className="mono name">
                {c.repo}/{c.name}
              </div>
              <Badges c={c} />
            </div>
          );
        })}
        {!d.capsules.length && <div className="dim">None yet.</div>}
      </SideSection>
      <SideSection title="Crew" count={live.length ? `${live.length} on it` : undefined}>
        {live.map((c) => (
          // What it said is on Summary.
          <CrewLine key={c.id} c={c} noReply />
        ))}
        {d.crew.length > live.length && (
          <div className="dim">
            {live.length ? `${d.crew.length - live.length} earlier` : `${d.crew.length} so far, none on it now`} · 4 for the log
          </div>
        )}
        {!d.crew.length && <div className="dim">None yet.</div>}
      </SideSection>
    </aside>
  );
}

function SideSection({ title, count, children }: { title: string; count?: string | number; children: React.ReactNode }) {
  return (
    <section className="side-section">
      <h3>
        {title}
        {count !== undefined && <span className="dim"> {count}</span>}
      </h3>
      {children}
    </section>
  );
}

const summaryArtifacts = 6;
const sideArtifacts = 8;
const summaryRecent = 6;
const summaryRecentWithSide = 12;

// withPRs keeps the PRs already known while what's on disk refreshes.
function withPRs(next: CapsuleDetail[], known: CapsuleDetail[]): CapsuleDetail[] {
  return next.map((c) => ({ ...c, pr: c.pr ?? known.find((k) => k.repo === c.repo && k.name === c.name)?.pr }));
}

function byKind(list: Artifact[]): [string, Artifact[]][] {
  const groups = new Map<string, Artifact[]>();
  for (const a of list) groups.set(a.kind, [...(groups.get(a.kind) ?? []), a]);
  return [...groups];
}

interface Parts {
  d: MissionDetail;
  selected: (key: string) => string;
  openArtifact: (a: Artifact) => void;
  review: (c: CapsuleDetail) => void;
}

// Summary is what needs you, then the rest in a line or two. With the
// sidebar showing, stages, crew, capsules and artifacts are there, and
// this keeps to the goal, where the work is now, and what's happened.
function Summary({ d, selected, openArtifact, review, full }: Parts & { full: boolean }) {
  const goal = briefGoal(d.brief);
  const live = d.crew.filter((c) => !c.ended);
  const last = d.crew[d.crew.length - 1];
  const now = d.stages.find((s) => s.state === "current");
  const recent = full ? summaryRecent : summaryRecentWithSide;
  return (
    <>
      <NeedsYou d={d} />
      <dl className="summary">
        {goal && (
          <Row label="Goal">
            {goal} <span className="dim">· 2 for the brief</span>
          </Row>
        )}
        {!full && now && (
          <Row label="Now">
            <span title={now.text}>
              Stage {now.n} of {d.stages.length}: {now.title}
            </span>
            {now.crew?.length ? <span className="dim"> · {now.crew.join(", ")}</span> : null}
            {now.checkIn && <span className="dim"> · check in after</span>}
          </Row>
        )}
        {!full &&
          live
            .filter((c) => c.lastReply && c.activity !== "working")
            .map((c) => (
              <Row key={c.id} label={`${c.id} said`}>
                <div className="reply">{c.lastReply}</div>
              </Row>
            ))}
        {full && d.stages.length > 0 && (
          <Row label="Stages">
            <Stages d={d} />
          </Row>
        )}
        {full && d.artifacts.length > 0 && (
          <Row label="Artifacts">
            {d.artifacts.slice(0, summaryArtifacts).map((a) => (
              <ArtifactLine key={a.ref} a={a} selected={selected} open={openArtifact} />
            ))}
            {d.artifacts.length > summaryArtifacts && <div className="dim">+{d.artifacts.length - summaryArtifacts} more · 3 for the work</div>}
          </Row>
        )}
        {full && (
          <Row label="Capsules">
            {d.capsules.map((c) => (
              <CapsuleLine key={`${c.repo}/${c.name}`} c={c} selected={selected} review={review} />
            ))}
            {!d.capsules.length && <div className="dim">None yet.</div>}
          </Row>
        )}
        {full && (
          <Row label="Crew">
            {live.map((c) => (
              <CrewLine key={c.id} c={c} />
            ))}
            {!live.length && (
              <div className="dim">
                {d.crew.length ? `${d.crew.length} crew so far, last ${last.id} (${last.harness}), none working now` : "None yet: ⏎ in ws deck launches crew."}
              </div>
            )}
          </Row>
        )}
        {d.events.length > 0 && (
          <Row label="Recently">
            {d.events.slice(-recent).map((e, i) => (
              <div key={i} className="event">
                <span className="dim mono">{clock(e.time)}</span> {e.text}
              </div>
            ))}
            {d.events.length > recent && <div className="dim">4 for the whole log</div>}
          </Row>
        )}
        {d.from && (
          <Row label="Raised from">
            <span className="dim">{d.from}</span>
          </Row>
        )}
      </dl>
    </>
  );
}

function NeedsYou({ d }: { d: MissionDetail }) {
  return (
    <>
      {d.state === "briefing" && (
        <div className="callout">
          <strong>Being briefed.</strong> An agent is turning the request into a brief and capsules.
          {d.request && <div className="dim">Requested: {d.request}</div>}
        </div>
      )}
      {d.flag && (
        <div className="callout">
          <strong>{flagWho(d.flag.by)}:</strong> {d.flag.text}
          {d.flag.options && d.flag.options.length > 0 && (
            <ol className="options">
              {d.flag.options.map((o, i) => (
                <li key={i}>{o}</li>
              ))}
            </ol>
          )}
          <div className="dim">Answer in ws deck (⏎ on the mission), or ws mission answer.</div>
        </div>
      )}
      {d.dispatch && (
        <div className="callout">
          <strong>Next{d.dispatch.capsule ? ` in ${d.dispatch.capsule}` : ""}:</strong> {d.dispatch.text}{" "}
          <span className="dim">({d.dispatch.by}) · ⏎ in ws deck launches it</span>
        </div>
      )}
      {d.conclude && (
        <div className="callout ok">
          <strong>Suggested conclusion:</strong> {d.conclude.text} <span className="dim">({d.conclude.by})</span>
        </div>
      )}
    </>
  );
}

// flagWho says who raised a flag: the orchestrator, you, or crew.
function flagWho(by?: string) {
  if (!by || by === "orchestrator") return "The orchestrator asks";
  if (by === "you") return "You flagged";
  return `${by} asks`;
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="summary-row">
      <dt className="dim">{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function Stages({ d, compact }: { d: MissionDetail; compact?: boolean }) {
  return (
    <ol className={`stages${compact ? " compact" : ""}`}>
      {d.stages.map((s) => (
        <li key={s.n} className={`stage ${s.state}`} title={s.text}>
          <span className="mark">{s.state === "accepted" ? "✓" : s.state === "current" ? "▸" : "·"}</span>
          <span className="title">
            {s.n} {s.title}
          </span>
          {(s.crew?.length || (s.checkIn && s.state !== "accepted")) && (
            <span className="dim">
              {" · "}
              {[s.crew?.join(", "), s.checkIn && s.state !== "accepted" ? "check in after" : ""].filter(Boolean).join(" · ")}
            </span>
          )}
          {s.why && !compact && <div className="dim why">accepted: {s.why}</div>}
        </li>
      ))}
    </ol>
  );
}

function CrewLine({ c, noReply }: { c: CrewDetail; noReply?: boolean }) {
  const glyph = c.ended ? "○" : ({ working: "●", waiting: "◐", permission: "?" }[c.activity ?? "working"] ?? "●");
  const what = c.ended ? (c.status ?? c.exit ?? "ended") : (c.status ?? c.activity ?? "starting");
  return (
    <div className="crew-line">
      <span className={`chip ${c.ended ? "ended" : (c.activity ?? "")}`}>
        {glyph}
        {c.id} {what}
      </span>{" "}
      {c.role === "briefing" ? "briefing" : c.harness}
      {c.capsule && <span className="dim mono"> in {c.capsule}</span>}
      {c.note && <span>: {c.note}</span>}
      {!noReply && !c.note && c.lastReply && !c.ended && c.activity !== "working" && <div className="reply">{c.lastReply}</div>}
    </div>
  );
}

function CapsuleLine({ c, selected, review, full }: { c: CapsuleDetail; selected: (k: string) => string; review: (c: CapsuleDetail) => void; full?: boolean }) {
  const key = `c:${c.repo}/${c.name}`;
  return (
    <div id={`item-${key}`} className={`item capsule${selected(key)}`} onClick={() => review(c)}>
      <div className="item-line">
        <span className="mono">
          {c.repo}/{c.name}
        </span>
        {c.branch && c.branch !== c.name && <span className="dim mono">{c.branch}</span>}
        <Badges c={c} />
        <span className="spacer" />
        {c.exists && (
          <span className="dim hint">
            <kbd>v</kbd> review
          </span>
        )}
      </div>
      {full && c.exists && <div className="dim mono path">{c.path}</div>}
      {full && c.pr && <div className="dim">{c.pr.title}</div>}
    </div>
  );
}

// Badges are a capsule's state at a glance: burned, uncommitted changes,
// commits ahead and behind, its PR and checks.
function Badges({ c }: { c: CapsuleDetail }) {
  return (
    <span className="badges">
      {!c.exists && <span className="dim">burned</span>}
      {c.dirty && (
        <span className="dirty" title="uncommitted changes">
          ●
        </span>
      )}
      {!!c.ahead && (
        <span className="ahead" title={`${c.ahead} commits ahead`}>
          ↑{c.ahead}
        </span>
      )}
      {!!c.behind && (
        <span className="dim" title={`${c.behind} commits behind`}>
          ↓{c.behind}
        </span>
      )}
      {c.pr && (
        <a href={c.pr.url} target="_blank" rel="noopener noreferrer" className={c.pr.merged ? "merged" : "open"} onClick={(e) => e.stopPropagation()} title={c.pr.title}>
          #{c.pr.number}
          {c.pr.merged ? " merged" : ""}
        </a>
      )}
      {c.pr?.checks && <span className={`checks ${c.pr.checks.toLowerCase()}`}>{c.pr.checks.toLowerCase()}</span>}
    </span>
  );
}

const stateClass: Record<string, string> = { landed: "good", merged: "good", "in review": "busy", open: "busy", draft: "busy", missing: "bad" };

function ArtifactLine({ a, selected, open }: { a: Artifact; selected: (k: string) => string; open: (a: Artifact) => void }) {
  const key = `a:${a.ref}`;
  return (
    <div id={`item-${key}`} className={`item artifact${selected(key)}${a.readable || a.url ? " openable" : ""}`} onClick={() => open(a)}>
      <div className="item-line">
        <span className="dim kind-word">{a.kind}</span>
        <span className={`art-state ${stateClass[a.state] ?? ""}`}>{a.state}</span>
        <span className="name">{a.label || a.ref}</span>
        {a.label && <span className="dim mono ref">{a.ref}</span>}
        <span className="spacer" />
        {a.by && <span className="dim">{a.by}</span>}
        {a.readable ? <span className="dim hint">⏎ read</span> : a.url ? <span className="dim hint">⏎ open ↗</span> : null}
      </div>
    </div>
  );
}

function Brief({ d }: { d: MissionDetail }) {
  return (
    <div className="brief">
      {d.request && <div className="dim request">Requested: {d.request}</div>}
      {d.brief.trim() ? <Markdown text={d.brief} /> : <p className="dim">(empty)</p>}
      <p className="dim">Edit it in ws deck (e), or ws mission brief --set.</p>
    </div>
  );
}

function Work({ d, selected, openArtifact, review }: Parts) {
  return (
    <>
      <section>
        <h2 className="group">Capsules</h2>
        {d.capsules.map((c) => (
          <CapsuleLine key={`${c.repo}/${c.name}`} c={c} selected={selected} review={review} full />
        ))}
        {!d.capsules.length && <p className="dim">None yet: a in ws deck attaches one, or WS_MISSION={d.slug} ws lift &lt;repo&gt; &lt;name&gt;.</p>}
      </section>
      {byKind(d.artifacts).map(([kind, list]) => (
        <section key={kind}>
          <h2 className="group">{kind}</h2>
          {list.map((a) => (
            <ArtifactLine key={a.ref} a={a} selected={selected} open={openArtifact} />
          ))}
        </section>
      ))}
      {!d.artifacts.length && <p className="dim">No artifacts yet: crew record what they make with ws mission artifact add.</p>}
    </>
  );
}

function Log({ d, selected, openArtifact }: Omit<Parts, "review">) {
  return (
    <>
      <section>
        <h2 className="group">Log</h2>
        <ul className="log">
          {d.events.map((e, i) => (
            <li key={i}>
              <span className="dim mono">{stamp(e.time)}</span>
              <span>{e.text}</span>
            </li>
          ))}
          {!d.events.length && <li className="dim">Nothing yet.</li>}
        </ul>
      </section>
      {d.crew.length > 0 && (
        <section>
          <h2 className="group">Crew</h2>
          {[...d.crew].reverse().map((c) => (
            <div key={c.id} className="crew-detail">
              <CrewLine c={c} />
              <div className="dim">
                started {stamp(c.started)}
                {c.endedAt ? `, ended ${stamp(c.endedAt)}${c.exit ? ` (${c.exit})` : ""}` : ", running"}
              </div>
              {c.dispatch && <div className="dispatch">dispatched for: {c.dispatch}</div>}
              {c.handoff && (
                <div
                  id={`item-h:${c.id}`}
                  className={`item openable${selected(`h:${c.id}`)}`}
                  onClick={() => openArtifact({ ref: c.handoff!, readable: true } as Artifact)}
                >
                  <div className="item-line">
                    <span className="dim kind-word">handoff</span>
                    <span className="mono">{c.handoff}</span>
                    <span className="spacer" />
                    <span className="dim hint">⏎ read</span>
                  </div>
                </div>
              )}
            </div>
          ))}
        </section>
      )}
    </>
  );
}

function StateLabel({ state }: { state: MissionState }) {
  return (
    <span className={`state state-${state}`}>
      <span className="glyph" />
      <span>{state}</span>
    </span>
  );
}

// briefGoal is the brief's goal in a line: the first paragraph under a Goal
// heading, wherever it is, else the first paragraph that isn't a heading.
// (The deck's briefGoal, the same way.)
export function briefGoal(brief: string): string {
  const lines = brief.split("\n");
  const firstParagraph = (from: number) => {
    const para: string[] = [];
    for (const line of lines.slice(from)) {
      const t = line.trim();
      if ((t.startsWith("#") || t === "") && para.length) break;
      if (t === "" || t.startsWith("#")) continue;
      para.push(t);
    }
    return para.join(" ");
  };
  for (let i = 0; i < lines.length; i++) {
    const t = lines[i].trim();
    if (t.startsWith("#") && t.replace(/^#+/, "").trim().toLowerCase() === "goal") {
      const goal = firstParagraph(i + 1);
      if (goal) return goal;
    }
  }
  return firstParagraph(0);
}

function ago(iso: string): string {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
const stamp = (iso: string) => {
  const t = new Date(iso);
  const today = new Date().toDateString() === t.toDateString();
  return today ? clock(iso) : t.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
};
