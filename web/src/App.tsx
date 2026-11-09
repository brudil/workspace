import { useMemo, useState } from "react";
import { Link, Route, Routes, useLocation, useNavigate, useParams } from "react-router";
import { type Connection, type Crew, type Mission, type MissionState, NotSignedIn, useWorkspace } from "./api";
import { useKeys } from "./keys";
import { Review, reviewKeys } from "./review/Review";

export function App() {
  const { workspace, error, connection } = useWorkspace();
  const [help, setHelp] = useState(false);
  const location = useLocation();
  useKeys({ "?": () => setHelp(true) });
  const reviewing = location.pathname.includes("/review/");

  if (error instanceof NotSignedIn) {
    return (
      <Notice title="Not signed in">
        Open this workspace's web UI from your terminal with <code>ws web</code>, or <kbd>o</kbd> in <code>ws deck</code>.
      </Notice>
    );
  }
  return (
    <div className={`shell${reviewing ? " reviewing" : ""}`}>
      <aside className="sidebar">
        <Link to="/" className="workspace-name">
          {workspace?.name ?? "ws"}
        </Link>
        <nav>
          <Link to="/">Missions</Link>
        </nav>
        <div className="sidebar-foot">
          <ConnectionDot connection={connection} />
          <button className="link" onClick={() => setHelp(true)}>
            <kbd>?</kbd> keys
          </button>
        </div>
      </aside>
      <main>
        {error && <div className="banner">{error.message}</div>}
        {workspace && (
          <Routes>
            <Route path="/" element={<Missions missions={workspace.missions} />} />
            <Route path="/missions/:slug" element={<MissionPage missions={workspace.missions} />} />
            <Route path="/missions/:slug/review/:repo/:name" element={<Review workspace={workspace} />} />
            <Route path="*" element={<Notice title="Nothing here">That page doesn't exist.</Notice>} />
          </Routes>
        )}
      </main>
      {help && <Help keys={reviewing ? reviewKeys : listKeys} onClose={() => setHelp(false)} />}
    </div>
  );
}

function ConnectionDot({ connection }: { connection: Connection }) {
  const label = { connecting: "connecting…", live: "live", lost: "deck server gone: reconnecting…" }[connection];
  return (
    <span className={`connection ${connection}`} title={label}>
      <span className="dot" /> {label}
    </span>
  );
}

// Missions are grouped the way the deck groups them: what needs you first.
const groups: { title: string; test: (m: Mission) => boolean }[] = [
  { title: "Needs you", test: (m) => !!m.flag || m.crew.some((c) => !c.ended && c.activity === "permission") },
  { title: "Active", test: (m) => m.state === "active" || m.state === "review" },
  { title: "Briefing", test: (m) => m.state === "briefing" || m.state === "draft" },
  { title: "Parked", test: (m) => m.state === "parked" },
];
const past = (m: Mission) => m.state === "landed" || m.state === "archived";

let lastSelected = ""; // kept across visits to a mission and back

function Missions({ missions }: { missions: Mission[] }) {
  const navigate = useNavigate();
  const [showPast, setShowPast] = useState(false);
  const sections = useMemo(() => {
    const placed = new Set<string>();
    const out: { title: string; missions: Mission[] }[] = [];
    for (const g of groups) {
      const ms = missions.filter((m) => !placed.has(m.slug) && !past(m) && g.test(m));
      ms.forEach((m) => placed.add(m.slug));
      if (ms.length) out.push({ title: g.title, missions: ms });
    }
    if (showPast) {
      const ms = missions.filter((m) => !placed.has(m.slug) && past(m));
      if (ms.length) out.push({ title: "Past", missions: ms });
    }
    return out;
  }, [missions, showPast]);
  const order = sections.flatMap((s) => s.missions);
  const [selected, setSelected] = useState(lastSelected);
  const index = Math.max(0, order.findIndex((m) => m.slug === selected));
  const select = (i: number) => {
    const m = order[Math.min(Math.max(i, 0), order.length - 1)];
    if (m) {
      setSelected(m.slug);
      lastSelected = m.slug;
      document.getElementById(`mission-${m.slug}`)?.scrollIntoView({ block: "nearest" });
    }
  };
  const open = () => order[index] && navigate(`/missions/${order[index].slug}`);
  useKeys({
    j: () => select(index + 1),
    ArrowDown: () => select(index + 1),
    k: () => select(index - 1),
    ArrowUp: () => select(index - 1),
    g: () => select(0),
    G: () => select(order.length - 1),
    Enter: open,
    o: open,
    A: () => setShowPast((p) => !p),
  });

  if (!order.length) {
    return (
      <Notice title="No missions">
        Start one in <code>ws deck</code> with <kbd>n</kbd>.
      </Notice>
    );
  }
  return (
    <div className="page">
      <header className="page-head">
        <h1>Missions</h1>
        <span className="dim">
          <kbd>A</kbd> {showPast ? "hide" : "show"} past
        </span>
      </header>
      {sections.map((s) => (
        <section key={s.title}>
          <h2 className="group">
            {s.title} <span className="dim">{s.missions.length}</span>
          </h2>
          <ul className="rows">
            {s.missions.map((m) => (
              <li
                key={m.slug}
                id={`mission-${m.slug}`}
                className={`row ${order[index]?.slug === m.slug ? "selected" : ""}`}
                onMouseMove={() => order[index]?.slug !== m.slug && setSelected(m.slug)}
              >
                <Link to={`/missions/${m.slug}`} className="row-link" onClick={() => (lastSelected = m.slug)}>
                  <StateGlyph state={m.state} />
                  <span className="title">{m.title}</span>
                  <span className="dim mono">{m.slug}</span>
                  <span className="spacer" />
                  {m.crew
                    .filter((c) => !c.ended)
                    .map((c) => (
                      <CrewChip key={c.id} crew={c} />
                    ))}
                  <span className="dim mono">{m.capsules.map((c) => `${c.repo}/${c.name}`).join(" ")}</span>
                  <span className="dim age">{age(m.created)}</span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function MissionPage({ missions }: { missions: Mission[] }) {
  const { slug } = useParams();
  const navigate = useNavigate();
  const [sel, setSel] = useState(0);
  const m = missions.find((m) => m.slug === slug);
  const capsules = m?.capsules ?? [];
  const back = () => navigate("/");
  const review = (i: number) => {
    const c = capsules[i];
    if (m && c) navigate(`/missions/${m.slug}/review/${c.repo}/${c.name}`);
  };
  useKeys({
    Escape: back,
    Backspace: back,
    h: back,
    j: () => setSel((s) => Math.min(s + 1, capsules.length - 1)),
    k: () => setSel((s) => Math.max(s - 1, 0)),
    v: () => review(sel),
    Enter: () => review(sel),
  });
  if (!m) {
    return <Notice title="No such mission">{slug} isn't one of this workspace's missions.</Notice>;
  }
  return (
    <div className="page">
      <header className="page-head">
        <Link to="/" className="dim">
          Missions
        </Link>
        <span className="dim">/</span>
        <h1>{m.title}</h1>
        <StateGlyph state={m.state} label />
      </header>
      {m.flag && (
        <div className="callout">
          <strong>Needs you.</strong> {m.flag}
        </div>
      )}
      <section>
        <h2 className="group">Capsules</h2>
        <ul className="rows">
          {m.capsules.map((c, i) => (
            <li key={`${c.repo}/${c.name}`} className={`row ${i === sel ? "selected" : ""}`} onMouseMove={() => setSel(i)}>
              <Link to={`/missions/${m.slug}/review/${c.repo}/${c.name}`} className="row-link">
                <span className="mono">
                  {c.repo}/{c.name}
                </span>
                <span className="dim mono">{c.branch}</span>
                <span className="spacer" />
                {!c.exists && <span className="tag warn">missing</span>}
                {c.exists && i === sel && (
                  <span className="dim">
                    <kbd>v</kbd> review
                  </span>
                )}
              </Link>
            </li>
          ))}
          {!m.capsules.length && <li className="row static dim">No capsules yet.</li>}
        </ul>
      </section>
      <section>
        <h2 className="group">Crew</h2>
        <ul className="rows">
          {[...m.crew].reverse().map((c) => (
            <li key={c.id} className="row static crew-row">
              <div className="crew-line">
                <CrewChip crew={c} />
                <span className="dim">{c.role === "briefing" ? "briefing" : c.harness}</span>
                <span className="dim mono">{c.capsule}</span>
                <span className="spacer" />
                <span className="dim age">{age(c.started)}</span>
              </div>
              {c.lastReply && <p className="reply">{c.lastReply}</p>}
            </li>
          ))}
          {!m.crew.length && <li className="row static dim">No crew yet.</li>}
        </ul>
      </section>
    </div>
  );
}

function StateGlyph({ state, label }: { state: MissionState; label?: boolean }) {
  return (
    <span className={`state state-${state}`} title={state}>
      <span className="glyph" />
      {label && <span>{state}</span>}
    </span>
  );
}

function CrewChip({ crew }: { crew: Crew }) {
  const what = crew.ended ? (crew.status ?? "ended") : (crew.status ?? crew.activity ?? "starting");
  const glyph = crew.ended ? "○" : { working: "●", waiting: "◐", permission: "?" }[crew.activity ?? "working"] ?? "●";
  return (
    <span className={`chip ${crew.ended ? "ended" : (crew.activity ?? "")}`}>
      {glyph}
      {crew.id} {what}
    </span>
  );
}

const listKeys: [string, string][] = [
  ["j / k", "Next / previous"],
  ["g / G", "First / last"],
  ["⏎ or o", "Open"],
  ["v", "Review the capsule (on a mission)"],
  ["esc", "Back"],
  ["A", "Show or hide past missions"],
  ["?", "These keys"],
];

function Help({ keys, onClose }: { keys: [string, string][]; onClose: () => void }) {
  useKeys({ "?": onClose, Escape: onClose }, { modal: true });
  return (
    <div className="overlay" onClick={onClose}>
      <div className="dialog" onClick={(e) => e.stopPropagation()}>
        <h2>Keys</h2>
        <dl>
          {keys.map(([k, what]) => (
            <div key={k}>
              <dt>
                <kbd>{k}</kbd>
              </dt>
              <dd>{what}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}

function Notice({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="notice">
      <h1>{title}</h1>
      <p>{children}</p>
    </div>
  );
}

function age(iso: string): string {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}
