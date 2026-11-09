import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { capsuleAPI, type Diff, type DiffFile, getBlob, getDiff, getFileDiff, type Workspace } from "../api";
import { useKeys } from "../keys";
import { escapeHTML, highlightLines, languageOf, loadHighlighter } from "./highlight";
import { type Cell, type FileState, fileRows, isChange, navigable, type Row, sameLine } from "./model";

export const reviewKeys: [string, string][] = [
  ["j / k", "Next / previous line"],
  ["n / p", "Next / previous change"],
  ["J / K", "Next / previous file"],
  ["⌘D / ⌘U", "Down / up half a page"],
  ["g / G", "First / last"],
  ["t", "Jump to a file"],
  ["x", "Mark the file viewed (and go on)"],
  ["⏎", "Show hidden lines, or a large file"],
  ["e", "Show 20 more lines around this change"],
  ["u", "Unified / side by side"],
  ["r", "Reload"],
  ["esc", "Back to the mission"],
];

// Viewed files are kept per capsule in this browser, by the file's content:
// a file that changes again is unviewed.
const viewedKey = (capsule: string) => `ws:viewed:${capsule}`;

function loadViewed(capsule: string): Record<string, string> {
  try {
    return JSON.parse(localStorage.getItem(viewedKey(capsule)) ?? "{}");
  } catch {
    return {};
  }
}

function saveViewed(capsule: string, v: Record<string, string>) {
  try {
    localStorage.setItem(viewedKey(capsule), JSON.stringify(v));
  } catch {
    // private window: viewed lasts as long as the page
  }
}

const fileID = (f: DiffFile) => f.newBlob ?? f.oldBlob ?? f.status;

function loadSplit(): boolean {
  try {
    return localStorage.getItem("ws:split") === "1";
  } catch {
    return false;
  }
}

export function Review({ workspace }: { workspace: Workspace }) {
  const { slug = "", repo = "", name = "" } = useParams();
  const navigate = useNavigate();
  const capsule = `${slug}/${repo}/${name}`;
  const api = capsuleAPI(slug, repo, name);
  const mission = workspace.missions.find((m) => m.slug === slug);
  const current = mission?.capsules.find((c) => c.repo === repo && c.name === name);

  const [diff, setDiff] = useState<Diff>();
  const [error, setError] = useState<string>();
  const [viewed, setViewed] = useState(() => loadViewed(capsule));
  const [blobs, setBlobs] = useState<Record<string, string[]>>({});
  const [expanded, setExpanded] = useState<Record<string, FileState["expanded"]>>({});
  const [loadedLarge, setLoadedLarge] = useState<Record<string, DiffFile>>({});
  const [split, setSplit] = useState(loadSplit);
  const [cursor, setCursor] = useState("");
  const [finder, setFinder] = useState(false);
  const [highlighting, setHighlighting] = useState(false);

  const load = useCallback(() => {
    getDiff(api).then(
      (d) => {
        setDiff(d);
        setError(undefined);
      },
      (e: Error) => setError(e.message),
    );
  }, [api]);
  useEffect(load, [load]);
  useEffect(() => {
    loadHighlighter().then(() => setHighlighting(true));
  }, []);

  const files = useMemo(() => diff?.files.map((f) => loadedLarge[f.path] ?? f) ?? [], [diff, loadedLarge]);
  const states = useMemo(
    () =>
      files.map(
        (f): FileState => ({ viewed: viewed[f.path] === fileID(f), blob: blobs[f.path], expanded: expanded[f.path] ?? {} }),
      ),
    [files, viewed, blobs, expanded],
  );
  const rows = useMemo(() => files.map((f, i) => fileRows(i, f, states[i], split)), [files, states, split]);
  const nav = useMemo(() => rows.flat().filter(navigable), [rows]);
  const at = Math.max(0, nav.findIndex((r) => r.key === cursor));
  const row = nav[at];

  // Keep the cursor on the same line when the layout changes under it, or
  // failing that (a gap it was on has opened), in the same place.
  const last = useRef<{ row?: Row; at: number }>({ at: 0 });
  useEffect(() => {
    if (!nav.length) return;
    const i = nav.findIndex((r) => r.key === cursor);
    if (i >= 0) {
      last.current = { row: nav[i], at: i };
      return;
    }
    const same = last.current.row && sameLine(nav, last.current.row);
    setCursor((same ?? nav[Math.min(last.current.at, nav.length - 1)]).key);
  }, [nav, cursor]);

  // Bring the cursor into view when it moves (not when the rows rebuild).
  // At the very first row, the page's top: the header and its warnings.
  const pane = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!row) return;
    if (at === 0) {
      pane.current?.scrollTo({ top: 0 });
      return;
    }
    const el = row.type === "file" ? document.getElementById(`file-${row.file}`) : document.getElementById(`row-${row.key}`);
    el?.scrollIntoView({ block: row.type === "file" ? "start" : "nearest" });
  }, [row?.key]);

  const move = (to: number) => {
    const r = nav[Math.min(Math.max(to, 0), nav.length - 1)];
    if (r) setCursor(r.key);
  };
  const findFrom = (from: number, step: number, test: (r: Row, i: number) => boolean) => {
    for (let i = from + step; i >= 0 && i < nav.length; i += step) if (test(nav[i], i)) return move(i);
  };

  const loadBlob = async (f: DiffFile) => {
    if (blobs[f.path] || !diff) return blobs[f.path];
    const lines = await getBlob(api, diff.head, f.path);
    setBlobs((b) => ({ ...b, [f.path]: lines }));
    return lines;
  };
  // expand shows up to n more lines on each side of a gap (all of it if n
  // is 0), loading the file first.
  const expand = async (file: number, gap: number, n: number) => {
    const f = files[file];
    try {
      await loadBlob(f);
    } catch (e) {
      setError((e as Error).message);
      return;
    }
    setExpanded((x) => {
      const e = x[f.path]?.[gap] ?? { top: 0, bottom: 0 };
      // Before the first hunk, lines open upwards from it; after the last,
      // downwards; between two, from both.
      const first = gap === 0;
      const last = gap === f.hunks.length;
      const next = n === 0 ? { top: 1e9, bottom: 0 } : { top: first ? 0 : e.top + n, bottom: last ? 0 : e.bottom + n };
      return { ...x, [f.path]: { ...x[f.path], [gap]: next } };
    });
  };
  const showLarge = (f: DiffFile) => {
    if (!diff) return;
    getFileDiff(api, diff.base, diff.head, f.path).then(
      (full) => setLoadedLarge((l) => ({ ...l, [f.path]: full })),
      (e: Error) => setError(e.message),
    );
  };
  const toggleViewed = (file: number) => {
    const f = files[file];
    const next = { ...viewed };
    const marking = viewed[f.path] !== fileID(f);
    if (marking) next[f.path] = fileID(f);
    else delete next[f.path];
    setViewed(next);
    saveViewed(capsule, next);
    if (marking) {
      const after = nav.findIndex((r) => r.type === "file" && r.file > file && viewed[files[r.file].path] !== fileID(files[r.file]));
      setCursor(after >= 0 ? nav[after].key : `${file}`);
    } else setCursor(`${file}`);
  };
  const activate = (r: Row) => {
    if (r.type === "gap") expand(r.file, r.gap, r.count !== undefined && r.count <= 40 ? 0 : 20);
    else if (r.type === "note" && r.large) showLarge(files[r.file]);
    else if (r.type === "file") toggleViewed(r.file);
  };

  // FileSection only redraws when its own rows or cursor change, so it gets
  // callbacks that stay the same.
  const activateRef = useRef(activate);
  activateRef.current = activate;
  const onActivate = useCallback((r: Row) => activateRef.current(r), []);

  useKeys({
    j: () => move(at + 1),
    ArrowDown: () => move(at + 1),
    k: () => move(at - 1),
    ArrowUp: () => move(at - 1),
    "mod+d": () => move(at + 20),
    "mod+u": () => move(at - 20),
    g: () => move(0),
    G: () => move(nav.length - 1),
    n: () => findFrom(at, 1, (r, i) => isChange(r) && !(i > 0 && isChange(nav[i - 1]) && nav[i - 1].file === r.file)),
    p: () => {
      // To the start of the change before the one the cursor is in.
      let i = at;
      while (i > 0 && isChange(nav[i]) && isChange(nav[i - 1]) && nav[i - 1].file === nav[i].file) i--;
      findFrom(i, -1, (r, j) => isChange(r) && !(j > 0 && isChange(nav[j - 1]) && nav[j - 1].file === r.file));
    },
    J: () => findFrom(at, 1, (r) => r.type === "file"),
    K: () => {
      const start = nav.findLastIndex((r, i) => i <= at && r.type === "file");
      findFrom(start === at ? at : start + 1, -1, (r) => r.type === "file");
    },
    x: () => row && toggleViewed(row.file),
    Enter: () => row && activate(row),
    e: () => {
      if (!row || row.type !== "line") return;
      // The gaps either side of the hunk the cursor is in.
      const fileNav = rows[row.file];
      const i = fileNav.findIndex((r) => r.key === row.key);
      const before = fileNav.slice(0, i).findLast((r) => r.type === "gap");
      const after = fileNav.slice(i).find((r) => r.type === "gap");
      if (before?.type === "gap") expand(row.file, before.gap, 20);
      if (after?.type === "gap") expand(row.file, after.gap, 20);
    },
    u: () => {
      setSplit((s) => {
        try {
          localStorage.setItem("ws:split", s ? "0" : "1");
        } catch {
          // fine
        }
        return !s;
      });
    },
    t: () => setFinder(true),
    r: load,
    Escape: () => navigate(`/missions/${slug}`),
  });

  if (!mission || !current) {
    return (
      <div className="page">
        <h1>No such capsule</h1>
        <p className="dim">
          {slug} has no capsule {repo}/{name}.
        </p>
      </div>
    );
  }
  const totals = files.reduce((t, f) => ({ a: t.a + f.additions, d: t.d + f.deletions }), { a: 0, d: 0 });
  const viewedCount = states.filter((s) => s.viewed).length;
  const moved = diff && current.head && current.head !== diff.head;

  return (
    <div className="review">
      <aside className="files">
        <div className="files-head">
          <span>
            {files.length} files · {viewedCount} viewed
          </span>
          <span className="dim">
            <kbd>t</kbd>
          </span>
        </div>
        <div className="progress">
          <div style={{ width: files.length ? `${(100 * viewedCount) / files.length}%` : 0 }} />
        </div>
        <ul>
          {files.map((f, i) => (
            <li
              key={f.path}
              className={`${row?.file === i ? "here" : ""} ${states[i].viewed ? "viewed" : ""}`}
              onClick={() => setCursor(`${i}`)}
            >
              <span className={`status s-${f.status}`}>{f.status[0].toUpperCase()}</span>
              <span className="path">
                <span className="dim">{dirOf(f.path)}</span>
                {baseOf(f.path)}
              </span>
              <span className="counts">
                {f.additions > 0 && <span className="plus">+{f.additions}</span>}
                {f.deletions > 0 && <span className="minus">−{f.deletions}</span>}
              </span>
              <span className="check">{states[i].viewed ? "✓" : ""}</span>
            </li>
          ))}
        </ul>
      </aside>
      <div className="diff-pane" ref={pane}>
        <header className="review-head">
          <Link to={`/missions/${slug}`} className="dim">
            {mission.title}
          </Link>
          <span className="dim">/</span>
          <h1 className="mono">
            {repo}/{name}
          </h1>
          {diff && (
            <span className="dim">
              {diff.commits.length} {diff.commits.length === 1 ? "commit" : "commits"} ahead of {diff.baseRef} ·{" "}
              <span className="plus">+{totals.a}</span> <span className="minus">−{totals.d}</span>
            </span>
          )}
          <span className="dim spacer-left">
            <kbd>u</kbd> {split ? "side by side" : "unified"}
          </span>
        </header>
        {moved && (
          <div className="callout">
            The capsule has new commits since this loaded. <kbd>r</kbd> reloads.
          </div>
        )}
        {diff && diff.uncommitted.length > 0 && (
          <div className="callout">
            {diff.uncommitted.length} {diff.uncommitted.length === 1 ? "file has" : "files have"} uncommitted changes, not in
            this review: <span className="mono">{diff.uncommitted.slice(0, 5).join(", ")}</span>
            {diff.uncommitted.length > 5 && " …"}
          </div>
        )}
        {error && <div className="banner">{error}</div>}
        {diff && diff.commits.length > 0 && (
          <details className="commits">
            <summary className="dim">Commits</summary>
            <ul>
              {diff.commits.map((c) => (
                <li key={c.sha}>
                  <span className="mono dim">{c.sha.slice(0, 7)}</span> {c.subject} <span className="dim">· {c.author}</span>
                </li>
              ))}
            </ul>
          </details>
        )}
        {diff && !files.length && <p className="dim">No committed changes yet.</p>}
        {files.map((f, i) => (
          <FileSection
            key={f.path}
            index={i}
            file={f}
            rows={rows[i]}
            state={states[i]}
            split={split}
            cursor={row?.file === i ? cursor : ""}
            highlighting={highlighting}
            onRow={setCursor}
            onActivate={onActivate}
          />
        ))}
      </div>
      {finder && (
        <Finder
          files={files}
          onPick={(i) => {
            setFinder(false);
            setCursor(`${i}`);
          }}
          onClose={() => setFinder(false)}
        />
      )}
    </div>
  );
}

const dirOf = (p: string) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/") + 1) : "");
const baseOf = (p: string) => p.slice(p.lastIndexOf("/") + 1);

interface Highlighted {
  old: Map<number, string>;
  new: Map<number, string>;
  shown: string[];
}

// highlight colours a file's lines: each hunk's sides as wholes, and the
// file at head for lines opened up from gaps.
function highlight(f: DiffFile, blob: string[] | undefined): Highlighted {
  const lang = languageOf(f.path);
  const out: Highlighted = { old: new Map(), new: new Map(), shown: [] };
  for (const h of f.hunks) {
    const olds = h.lines.filter((l) => l.k !== "a");
    const news = h.lines.filter((l) => l.k !== "d");
    highlightLines(olds.map((l) => l.t), lang).forEach((html, i) => out.old.set(olds[i].o!, html));
    highlightLines(news.map((l) => l.t), lang).forEach((html, i) => out.new.set(news[i].n!, html));
  }
  if (blob) out.shown = highlightLines(blob, lang);
  return out;
}

interface FileSectionProps {
  index: number;
  file: DiffFile;
  rows: Row[];
  state: FileState;
  split: boolean;
  cursor: string; // the cursor's row, if it's in this file
  highlighting: boolean;
  onRow: (key: string) => void;
  onActivate: (r: Row) => void;
}

const FileSection = memo(function FileSection({ index, file, rows, state, split, cursor, highlighting, onRow, onActivate }: FileSectionProps) {
  // highlighting is a dependency: once the highlighter loads, colour.
  const hl = useMemo(() => highlight(file, state.blob), [file, state.blob, highlighting]);
  const html = (c: Cell) =>
    (c.shown ? hl.shown[(c.at ?? c.num) - 1] : c.kind === "d" ? hl.old.get(c.num) : hl.new.get(c.num)) ?? escapeHTML(c.text);
  return (
    <section id={`file-${index}`} className={`file ${state.viewed ? "viewed" : ""}`}>
      <div
        id={`row-${index}`}
        className={`file-head ${cursor === `${index}` ? "cursor" : ""}`}
        onClick={() => onRow(`${index}`)}
      >
        <span className={`status s-${file.status}`}>{file.status}</span>
        <span className="mono">
          {file.oldPath && <span className="dim">{file.oldPath} → </span>}
          {file.path}
        </span>
        <span className="counts">
          {file.additions > 0 && <span className="plus">+{file.additions}</span>}{" "}
          {file.deletions > 0 && <span className="minus">−{file.deletions}</span>}
        </span>
        <label className="viewed-box" onClick={(e) => e.stopPropagation()}>
          <input type="checkbox" checked={state.viewed} onChange={() => onActivate({ type: "file", key: `${index}`, file: index })} />
          Viewed
        </label>
      </div>
      {!state.viewed && (
        <div className={`lines ${split ? "split" : "unified"}`}>
          {rows.slice(1).map((r) => (
            <RowView key={r.key} row={r} split={split} cursor={cursor === r.key} html={html} onRow={onRow} onActivate={onActivate} />
          ))}
        </div>
      )}
    </section>
  );
});

interface RowViewProps {
  row: Row;
  split: boolean;
  cursor: boolean;
  html: (c: Cell) => string;
  onRow: (key: string) => void;
  onActivate: (r: Row) => void;
}

function RowView({ row, split, cursor, html, onRow, onActivate }: RowViewProps) {
  const cls = cursor ? " cursor" : "";
  if (row.type === "gap") {
    return (
      <div id={`row-${row.key}`} className={`gap${cls}`} onClick={() => onActivate(row)}>
        <span className="gap-mark">⋯</span>
        <span>{row.count === undefined ? "Show more" : `${row.count} unchanged ${row.count === 1 ? "line" : "lines"}`}</span>
        {row.section && <span className="mono dim">{row.section}</span>}
      </div>
    );
  }
  if (row.type === "note") {
    return (
      <div id={`row-${row.key}`} className={`note${cls}${row.large ? " clickable" : ""}`} onClick={() => row.large && onActivate(row)}>
        {row.text}
      </div>
    );
  }
  if (row.type !== "line") return null;
  const code = (c?: Cell) =>
    c ? (
      <code className={`${c.kind}${c.shown ? " shown" : ""}`} dangerouslySetInnerHTML={{ __html: html(c) + (c.noeol ? '<span class="noeol">⊘</span>' : "") }} />
    ) : (
      <code className="empty" />
    );
  if (split) {
    return (
      <div id={`row-${row.key}`} className={`dl${cls}`} onClick={() => onRow(row.key)}>
        <span className={`ln ${row.left?.kind ?? "empty"}`}>{row.left?.num}</span>
        {code(row.left)}
        <span className={`ln ${row.right?.kind ?? "empty"}`}>{row.right?.num}</span>
        {code(row.right)}
      </div>
    );
  }
  const c = row.right ?? row.left!;
  const kind = row.left && !row.right ? "d" : row.right && !row.left ? "a" : "c";
  return (
    <div id={`row-${row.key}`} className={`dl ${kind}${c.shown ? " shown" : ""}${cls}`} onClick={() => onRow(row.key)}>
      <span className="ln">{row.left?.num}</span>
      <span className="ln">{row.right?.num}</span>
      <span className="sign">{kind === "a" ? "+" : kind === "d" ? "−" : ""}</span>
      {code({ ...c, kind })}
    </div>
  );
}

// Finder jumps to a file by a few letters of its path.
function Finder({ files, onPick, onClose }: { files: DiffFile[]; onPick: (i: number) => void; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  useKeys({ Escape: onClose }, { modal: true });
  const matches = useMemo(() => {
    const needle = q.toLowerCase();
    return files
      .map((f, i) => ({ f, i, score: fuzzy(f.path.toLowerCase(), needle) }))
      .filter((m) => m.score >= 0)
      .sort((a, b) => a.score - b.score)
      .slice(0, 50);
  }, [files, q]);
  const pick = matches[Math.min(sel, matches.length - 1)];
  return (
    <div className="overlay" onClick={onClose}>
      <div className="dialog finder" onClick={(e) => e.stopPropagation()}>
        <input
          autoFocus
          value={q}
          placeholder="Jump to a file"
          onChange={(e) => {
            setQ(e.target.value);
            setSel(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n")) {
              e.preventDefault();
              setSel((s) => Math.min(s + 1, matches.length - 1));
            } else if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p")) {
              e.preventDefault();
              setSel((s) => Math.max(s - 1, 0));
            } else if (e.key === "Enter" && pick) {
              e.preventDefault();
              onPick(pick.i);
            }
          }}
        />
        <ul>
          {matches.map((m) => (
            <li key={m.f.path} className={m === pick ? "selected" : ""} onClick={() => onPick(m.i)}>
              <span className="mono">{m.f.path}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

// fuzzy scores how well needle's letters appear in order in hay: lower is
// better, -1 is no match. Letters together, and at the end, score better.
function fuzzy(hay: string, needle: string): number {
  if (!needle) return 0;
  let score = 0;
  let from = 0;
  for (const ch of needle) {
    const i = hay.indexOf(ch, from);
    if (i < 0) return -1;
    score += i - from;
    from = i + 1;
  }
  return score + (hay.length - from) * 0.1;
}
