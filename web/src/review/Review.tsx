import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import {
  addNote,
  capsuleAPI,
  deleteNote,
  type Diff,
  type DiffFile,
  discardReview,
  editNote,
  getBlob,
  getDiff,
  getFileDiff,
  getReview,
  type NoteDraft,
  type NoteKind,
  noteKinds,
  type Review as ReviewData,
  type ReviewState,
  setViewed,
  submitReview,
  type Verdict,
  type Workspace,
} from "../api";
import { useKeys } from "../keys";
import { escapeHTML, highlightLines, languageOf, loadHighlighter } from "./highlight";
import { type Cell, type Editing, type FileState, fileRows, isChange, navigable, type Placed, type Row, sameLine, type Target } from "./model";

export const reviewKeys: [string, string][] = [
  ["j / k", "Next / previous line"],
  ["n / p", "Next / previous change"],
  ["J / K", "Next / previous file"],
  ["⌘D / ⌘U", "Down / up half a page"],
  ["g / G", "First / last"],
  ["t", "Jump to a file"],
  ["c", "Comment on the line (or the selected lines)"],
  ["⇧↓ / ⇧↑", "Select lines (or V then move, or drag the line numbers)"],
  ["⏎", "Edit a draft comment; show hidden lines or a large file"],
  ["D", "Delete a draft comment"],
  ["s", "Submit the review"],
  ["x", "Mark the file viewed (and go on)"],
  ["e", "Show 20 more lines around this change"],
  ["u", "Unified / side by side"],
  ["r", "Reload"],
  ["esc", "Clear the selection, or back to the mission"],
];

const fileID = (f: DiffFile) => f.newBlob ?? f.oldBlob ?? f.status;

function loadSplit(): boolean {
  try {
    return localStorage.getItem("ws:split") === "1";
  } catch {
    return false;
  }
}

const verdictWords: Record<Verdict, string> = { approve: "approved", changes: "changes requested", comment: "comments" };

// targetOf is the lines a comment on these rows is on: the side picked
// (side by side, the column dragged), else the first line's (new, or
// removed if it was removed), and every line of that side among them.
// Comments on the old side are on removed lines: with none, it's the new.
function targetOf(rows: Row[], picked?: "old" | "new"): Target | undefined {
  const lines = rows.filter((r) => r.type === "line");
  const first = lines[0];
  if (first?.type !== "line") return undefined;
  const nums = (side: "old" | "new") =>
    lines.flatMap((r) => {
      if (r.type !== "line") return [];
      const c = side === "new" ? r.right : r.left?.kind === "d" ? r.left : undefined;
      return c ? [c.num] : [];
    });
  let side: "old" | "new" = picked ?? (first.right ? "new" : "old");
  let ns = nums(side);
  if (!ns.length && side === "old") [side, ns] = ["new", nums("new")];
  return ns.length ? { side, line: Math.min(...ns), endLine: Math.max(...ns) } : undefined;
}

const targetWords = (t: Target) =>
  `${t.side === "old" ? "Removed l" : "L"}${t.endLine > t.line ? `ines ${t.line}–${t.endLine}` : `ine ${t.line}`}`;

export function Review({ workspace }: { workspace: Workspace }) {
  const { slug = "", repo = "", name = "" } = useParams();
  const navigate = useNavigate();
  const api = capsuleAPI(slug, repo, name);
  const mission = workspace.missions.find((m) => m.slug === slug);
  const current = mission?.capsules.find((c) => c.repo === repo && c.name === name);

  const [diff, setDiff] = useState<Diff>();
  const [rs, setRS] = useState<ReviewState>({ draft: null, reviews: [], viewed: {} });
  const [error, setError] = useState<string>();
  const [blobs, setBlobs] = useState<Record<string, string[]>>({});
  const [expanded, setExpanded] = useState<Record<string, FileState["expanded"]>>({});
  const [loadedLarge, setLoadedLarge] = useState<Record<string, DiffFile>>({});
  const [split, setSplit] = useState(loadSplit);
  const [cursor, setCursor] = useState("");
  const [selectFrom, setSelectFrom] = useState<string>();
  const [selectSide, setSelectSide] = useState<"old" | "new">(); // side by side, the column dragged
  const [editing, setEditing] = useState<Editing>();
  const [finder, setFinder] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState<ReviewData>();
  const [highlighting, setHighlighting] = useState(false);

  const fail = (e: Error) => setError(e.message);
  const load = useCallback(() => {
    getDiff(api).then((d) => {
      setDiff(d);
      setError(undefined);
    }, fail);
    getReview(api).then(setRS, fail);
  }, [api]);
  useEffect(load, [load]);
  useEffect(() => {
    loadHighlighter().then(() => setHighlighting(true));
  }, []);

  const files = useMemo(() => diff?.files.map((f) => loadedLarge[f.path] ?? f) ?? [], [diff, loadedLarge]);
  const states = useMemo(
    () => files.map((f): FileState => ({ viewed: rs.viewed[f.path] === fileID(f), blob: blobs[f.path], expanded: expanded[f.path] ?? {} })),
    [files, rs.viewed, blobs, expanded],
  );
  // Comments to show: the draft's, and submitted ones made on these very
  // lines (comments on earlier commits are listed with their reviews).
  const notes = useMemo(() => {
    const byPath: Record<string, Placed[]> = {};
    if (!diff) return byPath;
    const add = (r: ReviewData, draft: boolean) => {
      for (const n of r.comments) {
        const commit = n.side === "new" ? diff.head : diff.base;
        if (!draft && n.commit !== commit) continue;
        (byPath[n.path] ??= []).push({ note: n, review: r.id, draft, stale: n.commit !== commit });
      }
    };
    [...rs.reviews].reverse().forEach((r) => add(r, false));
    if (rs.draft) add(rs.draft, true);
    return byPath;
  }, [rs, diff]);
  const rows = useMemo(
    () => files.map((f, i) => fileRows(i, f, states[i], split, notes[f.path], editing?.file === i ? editing : undefined)),
    [files, states, split, notes, editing],
  );
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

  // The selection runs from where V was pressed to the cursor, within a
  // file.
  const selection = useMemo(() => {
    if (!selectFrom) return undefined;
    const a = nav.findIndex((r) => r.key === selectFrom);
    if (a < 0 || !row || nav[a].file !== row.file) return undefined;
    const [lo, hi] = a <= at ? [a, at] : [at, a];
    return { file: row.file, keys: new Set(nav.slice(lo, hi + 1).map((r) => r.key)) };
  }, [selectFrom, nav, at, row]);

  const move = (to: number) => {
    const r = nav[Math.min(Math.max(to, 0), nav.length - 1)];
    if (r) setCursor(r.key);
  };
  const findFrom = (from: number, step: number, test: (r: Row, i: number) => boolean) => {
    for (let i = from + step; i >= 0 && i < nav.length; i += step) if (test(nav[i], i)) return move(i);
  };
  // A change starts at a changed line whose line before (comments aside)
  // isn't one of the same file's changes.
  const lineBefore = (i: number) => {
    for (let j = i - 1; j >= 0; j--) if (nav[j].type !== "comment") return nav[j];
    return undefined;
  };
  const startsChange = (i: number) => {
    const before = lineBefore(i);
    return isChange(nav[i]) && !(before && isChange(before) && before.file === nav[i].file);
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
      fail(e as Error);
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
    getFileDiff(api, diff.base, diff.head, f.path).then((full) => setLoadedLarge((l) => ({ ...l, [f.path]: full })), fail);
  };
  const toggleViewed = (file: number) => {
    const f = files[file];
    const marking = rs.viewed[f.path] !== fileID(f);
    setRS((s) => {
      const viewed = { ...s.viewed };
      if (marking) viewed[f.path] = fileID(f);
      else delete viewed[f.path];
      return { ...s, viewed };
    });
    setViewed(api, f.path, fileID(f), marking).then(setRS, fail);
    if (marking) {
      const after = nav.findIndex((r) => r.type === "file" && r.file > file && rs.viewed[files[r.file].path] !== fileID(files[r.file]));
      setCursor(after >= 0 ? nav[after].key : `${file}`);
    } else setCursor(`${file}`);
  };

  const selectedRows = useMemo(() => (selection ? nav.filter((r) => selection.keys.has(r.key)) : []), [selection, nav]);
  const selectedTarget = useMemo(() => targetOf(selectedRows, selectSide), [selectedRows, selectSide]);

  // comment opens the editor on the selected lines, or the cursor's.
  const comment = () => {
    if (!diff) return;
    const target = selection ? selectedTarget : row?.type === "line" ? targetOf([row]) : undefined;
    const file = selection ? selection.file : row?.file;
    if (!target || file === undefined) return;
    setEditing({ file, target });
    select(undefined);
  };
  // select starts (or, with no key, ends) a selection.
  const select = (key?: string, side?: "old" | "new") => {
    setSelectFrom(key);
    setSelectSide(side);
  };

  // Selecting lines: shift and the arrows from the keyboard (V and j/k too);
  // with the mouse, dragging the line numbers, or a click on one, and
  // shift-click to extend. A plain click elsewhere ends it.
  const sameFile = (a?: string, b?: string) => a !== undefined && b !== undefined && a.split(":")[0] === b.split(":")[0];
  // extend grows the selection by a line: over comments, but not across a
  // gap (the lines either side of one aren't together) or out of the file.
  const extend = (step: number) => {
    if (row?.type !== "line") return;
    let i = at + step;
    while (nav[i]?.type === "comment") i += step;
    const next = nav[i];
    if (next?.type !== "line" || next.file !== row.file) return;
    if (!selectFrom) select(row.key);
    setCursor(next.key);
  };
  const dragging = useRef(false);
  useEffect(() => {
    const up = () => (dragging.current = false);
    window.addEventListener("mouseup", up);
    return () => window.removeEventListener("mouseup", up);
  }, []);
  const rowClick = (key: string, shift: boolean) => {
    if (shift && sameFile(cursor, key)) {
      if (!selectFrom) select(cursor);
    } else select(undefined);
    setCursor(key);
  };
  const gutterDown = (key: string, shift: boolean, side?: "old" | "new") => {
    dragging.current = true;
    if (!(shift && sameFile(cursor, key))) select(key, side);
    else if (!selectFrom) select(cursor, side);
    setCursor(key);
  };
  const dragOver = (key: string) => {
    if (dragging.current && sameFile(selectFrom, key)) setCursor(key);
  };
  const activate = (r: Row) => {
    if (r.type === "gap") expand(r.file, r.gap, r.count !== undefined && r.count <= 40 ? 0 : 20);
    else if (r.type === "note" && r.large) showLarge(files[r.file]);
    else if (r.type === "file") toggleViewed(r.file);
    else if (r.type === "comment" && r.placed.draft) {
      const n = r.placed.note;
      setEditing({ file: r.file, target: n, noteID: n.id, note: n });
    }
  };
  const saveNote = async (n: NoteDraft) => {
    if (!editing || !diff) return;
    const f = files[editing.file];
    try {
      const next = editing.noteID
        ? await editNote(api, editing.noteID, n)
        : await addNote(api, { base: diff.base, head: diff.head }, { ...n, path: f.path, ...editing.target });
      setRS(next);
      closeEditor();
      setError(undefined);
    } catch (e) {
      fail(e as Error);
    }
  };
  // closeEditor puts the cursor back on the comment it was rewriting (the
  // editor stood in for it, so the cursor had to go elsewhere meanwhile).
  const closeEditor = () => {
    if (editing?.noteID) setCursor(`${editing.file}:c:${editing.noteID}`);
    setEditing(undefined);
  };
  const removeNote = (r: Row) => {
    if (r.type !== "comment" || !r.placed.draft) return;
    deleteNote(api, r.placed.note.id).then(setRS, fail);
  };

  // Stable callbacks: FileSection only redraws when its own rows, cursor or
  // selection change.
  const handlers = useRef({ activate, saveNote, cancel: closeEditor, comment, rowClick, gutterDown, dragOver });
  handlers.current = { activate, saveNote, cancel: closeEditor, comment, rowClick, gutterDown, dragOver };
  const onActivate = useCallback((r: Row) => handlers.current.activate(r), []);
  const onSave = useCallback((n: NoteDraft) => handlers.current.saveNote(n), []);
  const onCancel = useCallback(() => handlers.current.cancel(), []);
  const onComment = useCallback(() => handlers.current.comment(), []);
  const onRowClick = useCallback((key: string, shift: boolean) => handlers.current.rowClick(key, shift), []);
  const onGutterDown = useCallback((key: string, shift: boolean, side?: "old" | "new") => handlers.current.gutterDown(key, shift, side), []);
  const onDragOver = useCallback((key: string) => handlers.current.dragOver(key), []);

  useKeys({
    j: () => move(at + 1),
    ArrowDown: () => move(at + 1),
    k: () => move(at - 1),
    ArrowUp: () => move(at - 1),
    "mod+d": () => move(at + 20),
    "mod+u": () => move(at - 20),
    g: () => move(0),
    G: () => move(nav.length - 1),
    n: () => findFrom(at, 1, (_, i) => startsChange(i)),
    p: () => {
      // To the start of the change before the one the cursor is in.
      let i = at;
      while (i > 0 && (nav[i].type === "comment" || (isChange(nav[i]) && !startsChange(i)))) i--;
      findFrom(i, -1, (_, j) => startsChange(j));
    },
    J: () => findFrom(at, 1, (r) => r.type === "file"),
    K: () => {
      const start = nav.findLastIndex((r, i) => i <= at && r.type === "file");
      findFrom(start === at ? at : start + 1, -1, (r) => r.type === "file");
    },
    x: () => row && toggleViewed(row.file),
    Enter: () => row && activate(row),
    c: comment,
    V: () => row?.type === "line" && select(selectFrom ? undefined : row.key),
    "shift+ArrowDown": () => extend(1),
    "shift+ArrowUp": () => extend(-1),
    D: () => row && removeNote(row),
    s: () => diff && setSubmitting(true),
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
    Escape: () => (selectFrom ? select(undefined) : navigate(`/missions/${slug}`)),
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
  const draft = rs.draft;

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
              {(notes[f.path]?.length ?? 0) > 0 && <span className="note-count">{notes[f.path].length}</span>}
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
          <button className="submit-button" onClick={() => diff && setSubmitting(true)}>
            {draft ? `Submit review · ${draft.comments.length}` : "Review"} <kbd>s</kbd>
          </button>
        </header>
        {submitted && (
          <div className="callout ok">
            Submitted {submitted.id}: {verdictWords[submitted.verdict!]}. It's the mission's now: the orchestrator is deciding what happens next.
          </div>
        )}
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
        {rs.reviews.length > 0 && <EarlierReviews reviews={rs.reviews} />}
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
            selected={selection?.file === i ? selection.keys : undefined}
            selectionEnd={selection?.file === i ? selectedRows.findLast((r) => r.type === "line")?.key : undefined}
            selectionLabel={selection?.file === i && selectedTarget ? targetWords(selectedTarget) : undefined}
            editing={editing?.file === i ? editing : undefined}
            highlighting={highlighting}
            onRow={setCursor}
            onActivate={onActivate}
            onSave={onSave}
            onCancel={onCancel}
            onComment={onComment}
            onRowClick={onRowClick}
            onGutterDown={onGutterDown}
            onDragOver={onDragOver}
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
      {submitting && diff && (
        <SubmitDialog
          draft={draft}
          onClose={() => setSubmitting(false)}
          onDiscard={() =>
            discardReview(api).then((next) => {
              setRS(next);
              setSubmitting(false);
            }, fail)
          }
          onSubmit={async (verdict, summary) => {
            const next = await submitReview(api, { base: diff.base, head: diff.head }, verdict, summary);
            setRS(next);
            setSubmitted(next.reviews[0]);
            setSubmitting(false);
          }}
        />
      )}
    </div>
  );
}

const dirOf = (p: string) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/") + 1) : "");
const baseOf = (p: string) => p.slice(p.lastIndexOf("/") + 1);

function EarlierReviews({ reviews }: { reviews: ReviewData[] }) {
  return (
    <details className="commits">
      <summary className="dim">Reviews: {reviews.map((r) => `${r.id} ${verdictWords[r.verdict!]}`).join(" · ")}</summary>
      {reviews.map((r) => (
        <div key={r.id} className="earlier">
          <div>
            <strong>{r.id}</strong>{" "}
            <span className="dim">
              {verdictWords[r.verdict!]} · {r.submitted && new Date(r.submitted).toLocaleString()}
            </span>
          </div>
          {r.summary && <p className="summary">{r.summary}</p>}
          <ul>
            {r.comments.map((n) => (
              <li key={n.id}>
                <span className={`kind k-${n.kind}`}>{n.kind}</span>{" "}
                <span className="mono dim">
                  {n.path}:{n.line}
                </span>{" "}
                {n.body}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </details>
  );
}

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
  selected?: Set<string>;
  selectionEnd?: string; // the selection's last row, where its bar goes
  selectionLabel?: string;
  editing?: Editing;
  highlighting: boolean;
  onRow: (key: string) => void;
  onActivate: (r: Row) => void;
  onSave: (n: NoteDraft) => void;
  onCancel: () => void;
  onComment: () => void;
  onRowClick: (key: string, shift: boolean) => void;
  onGutterDown: (key: string, shift: boolean, side?: "old" | "new") => void;
  onDragOver: (key: string) => void;
}

const FileSection = memo(function FileSection(props: FileSectionProps) {
  const { index, file, rows, state, split, cursor, selected, selectionEnd, selectionLabel, editing, highlighting } = props;
  const { onRow, onActivate, onSave, onCancel, onComment, onRowClick, onGutterDown, onDragOver } = props;
  // highlighting is a dependency: once the highlighter loads, colour.
  const hl = useMemo(() => highlight(file, state.blob), [file, state.blob, highlighting]);
  const html = (c: Cell) =>
    (c.shown ? hl.shown[(c.at ?? c.num) - 1] : c.kind === "d" ? hl.old.get(c.num) : hl.new.get(c.num)) ?? escapeHTML(c.text);
  // The lines the editor is on, for a suggestion to start from.
  const editorLines = () => {
    if (!editing) return [];
    if (editing.note) return editing.note.excerpt;
    const t = editing.target;
    const out: string[] = [];
    for (const r of rows) {
      if (r.type !== "line") continue;
      const c = t.side === "new" ? r.right : r.left?.kind === "d" ? r.left : undefined;
      if (c && c.num >= t.line && c.num <= t.endLine) out.push(c.text);
    }
    return out;
  };
  return (
    <section id={`file-${index}`} className={`file ${state.viewed ? "viewed" : ""}`}>
      <div id={`row-${index}`} className={`file-head ${cursor === `${index}` ? "cursor" : ""}`} onClick={() => onRow(`${index}`)}>
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
          {rows.slice(1).map((r) =>
            r.type === "editor" && editing ? (
              <NoteEditor key={r.key} editing={editing} lines={editorLines()} onSave={onSave} onCancel={onCancel} />
            ) : r.type === "comment" ? (
              <NoteCard key={r.key} row={r} cursor={cursor === r.key} onRow={onRow} />
            ) : (
              <RowView
                key={r.key}
                row={r}
                split={split}
                cursor={cursor === r.key}
                selected={!!selected?.has(r.key)}
                html={html}
                onRowClick={onRowClick}
                onGutterDown={onGutterDown}
                onDragOver={onDragOver}
                onActivate={onActivate}
                bar={
                  r.key === selectionEnd && selectionLabel ? (
                    // It floats over the lines below rather than pushing them
                    // down: nothing moves under the mouse while dragging.
                    <div className="select-bar" onClick={(e) => e.stopPropagation()}>
                      <span>{selectionLabel}</span>
                      <button className="primary" onMouseDown={(e) => e.preventDefault()} onClick={onComment}>
                        Comment <kbd>c</kbd>
                      </button>
                      <span className="dim">
                        <kbd>esc</kbd> clears
                      </span>
                    </div>
                  ) : undefined
                }
              />
            ),
          )}
        </div>
      )}
    </section>
  );
});

interface RowViewProps {
  row: Row;
  split: boolean;
  cursor: boolean;
  selected: boolean;
  html: (c: Cell) => string;
  onRowClick: (key: string, shift: boolean) => void;
  onGutterDown: (key: string, shift: boolean, side?: "old" | "new") => void;
  onDragOver: (key: string) => void;
  onActivate: (r: Row) => void;
  bar?: React.ReactNode; // the selection's bar, on its last row
}

function RowView({ row, split, cursor, selected, html, onRowClick, onGutterDown, onDragOver, onActivate, bar }: RowViewProps) {
  const cls = (cursor ? " cursor" : "") + (selected ? " selected" : "");
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
  // The line numbers are where lines are picked: press there and drag to
  // select, without selecting the text (which stays copyable).
  const gutter = (side?: "old" | "new") => ({
    onMouseDown: (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      e.preventDefault();
      onGutterDown(row.key, e.shiftKey, side);
    },
    onClick: (e: React.MouseEvent) => e.stopPropagation(),
  });
  const line = {
    id: `row-${row.key}`,
    onClick: (e: React.MouseEvent) => onRowClick(row.key, e.shiftKey),
    onMouseEnter: () => onDragOver(row.key),
  };
  if (split) {
    return (
      <div {...line} className={`dl${cls}`}>
        <span className={`ln gutter ${row.left?.kind ?? "empty"}`} {...gutter("old")}>
          {row.left?.num}
        </span>
        {code(row.left)}
        <span className={`ln gutter ${row.right?.kind ?? "empty"}`} {...gutter("new")}>
          {row.right?.num}
        </span>
        {code(row.right)}
        {bar}
      </div>
    );
  }
  const c = row.right ?? row.left!;
  const kind = row.left && !row.right ? "d" : row.right && !row.left ? "a" : "c";
  return (
    <div {...line} className={`dl ${kind}${c.shown ? " shown" : ""}${cls}`}>
      <span className="ln gutter" {...gutter()}>
        {row.left?.num}
      </span>
      <span className="ln gutter" {...gutter()}>
        {row.right?.num}
      </span>
      <span className="sign gutter" {...gutter()}>
        {kind === "a" ? "+" : kind === "d" ? "−" : ""}
      </span>
      {code({ ...c, kind })}
      {bar}
    </div>
  );
}

const linesWord = (t: Target) => (t.endLine > t.line ? `lines ${t.line}–${t.endLine}` : `line ${t.line}`);

function NoteCard({ row, cursor, onRow }: { row: Extract<Row, { type: "comment" }>; cursor: boolean; onRow: (key: string) => void }) {
  const { note, review, draft, stale } = row.placed;
  return (
    <div id={`row-${row.key}`} className={`note-card${cursor ? " cursor" : ""}${draft ? " draft" : ""}`} onClick={() => onRow(row.key)}>
      <div className="note-head">
        <span className={`kind k-${note.kind}`}>{note.kind}</span>
        <span className="dim">
          {draft ? "draft" : review} · {note.side === "old" ? "removed " : ""}
          {linesWord(note)}
          {(row.unplaced || stale) && " · its lines aren't showing here"}
        </span>
        {draft && cursor && (
          <span className="dim spacer-left">
            <kbd>⏎</kbd> edit <kbd>D</kbd> delete
          </span>
        )}
      </div>
      {note.body && <div className="note-body">{note.body}</div>}
      {note.suggestion != null && <Suggestion from={note.excerpt} to={note.suggestion} />}
    </div>
  );
}

function Suggestion({ from, to }: { from: string[]; to: string }) {
  return (
    <div className="suggestion">
      <div className="dim">Suggested change</div>
      {from.map((l, i) => (
        <div key={`d${i}`} className="sl d">
          − {l}
        </div>
      ))}
      {to === "" ? (
        <div className="sl dim">(delete these lines)</div>
      ) : (
        to.split("\n").map((l, i) => (
          <div key={`a${i}`} className="sl a">
            + {l}
          </div>
        ))
      )}
    </div>
  );
}

// A suggestion is written as a ```suggestion block in the comment, as on
// GitHub; it's kept apart from the words.
const suggestionBlock = /```suggestion\n([\s\S]*?)\n?```\n?/;

function splitSuggestion(text: string): { body: string; suggestion: string | null } {
  const m = text.match(suggestionBlock);
  if (!m) return { body: text.trim(), suggestion: null };
  return { body: text.replace(suggestionBlock, "").trim(), suggestion: m[1] };
}

function joinSuggestion(body: string, suggestion?: string | null) {
  return suggestion == null ? body : `${body}${body ? "\n\n" : ""}\`\`\`suggestion\n${suggestion}\n\`\`\``;
}

function NoteEditor({ editing, lines, onSave, onCancel }: { editing: Editing; lines: string[]; onSave: (n: NoteDraft) => void; onCancel: () => void }) {
  const note = editing.note;
  const [kind, setKind] = useState<NoteKind>(note?.kind ?? "suggestion");
  const [text, setText] = useState(() => (note ? joinSuggestion(note.body, note.suggestion) : ""));
  const area = useRef<HTMLTextAreaElement>(null);
  // While writing, the page's keys are off: these are the editor's.
  useKeys({}, { modal: true });
  useEffect(() => {
    area.current?.focus();
    area.current?.scrollIntoView({ block: "nearest" });
  }, []);
  const save = () => {
    const { body, suggestion } = splitSuggestion(text);
    if (!body && suggestion == null) return;
    onSave({ kind, body, suggestion });
  };
  const suggest = () => {
    if (suggestionBlock.test(text)) return;
    setText((t) => joinSuggestion(t.trim(), lines.join("\n")));
    area.current?.focus();
  };
  return (
    <div className="note-editor">
      <div className="note-head">
        {noteKinds.map((k) => (
          <button key={k} className={`kind k-${k}${k === kind ? " on" : ""}`} onClick={() => setKind(k)}>
            {k}
          </button>
        ))}
        <span className="dim">
          {editing.target.side === "old" ? "removed " : ""}
          {linesWord(editing.target)} · <kbd>tab</kbd> kind
        </span>
      </div>
      <textarea
        ref={area}
        value={text}
        rows={Math.min(12, Math.max(3, text.split("\n").length + 1))}
        placeholder="What should change, or what do you want to know?"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            save();
          } else if (e.key === "Escape") {
            e.preventDefault();
            onCancel();
          } else if (e.key === "Tab") {
            e.preventDefault();
            const i = noteKinds.indexOf(kind);
            setKind(noteKinds[(i + (e.shiftKey ? noteKinds.length - 1 : 1)) % noteKinds.length]);
          }
        }}
      />
      <div className="note-actions">
        <button onClick={suggest} disabled={suggestionBlock.test(text)}>
          Suggest a change
        </button>
        <span className="spacer-left" />
        <button onClick={onCancel}>
          Cancel <kbd>esc</kbd>
        </button>
        <button className="primary" onClick={save}>
          {note ? "Save" : "Comment"} <kbd>⌘⏎</kbd>
        </button>
      </div>
    </div>
  );
}

// SubmitDialog ends the review with a verdict: a, c or m picks it, then
// the summary, then ⌘⏎.
function SubmitDialog({
  draft,
  onClose,
  onDiscard,
  onSubmit,
}: {
  draft: ReviewData | null;
  onClose: () => void;
  onDiscard: () => void;
  onSubmit: (v: Verdict, summary: string) => Promise<void>;
}) {
  const [verdict, setVerdict] = useState<Verdict>();
  const [summary, setSummary] = useState("");
  const [error, setError] = useState<string>();
  const area = useRef<HTMLTextAreaElement>(null);
  const pick = (v: Verdict) => {
    setVerdict(v);
    setTimeout(() => area.current?.focus());
  };
  const submit = () => {
    if (!verdict) return setError("Pick a verdict first.");
    onSubmit(verdict, summary).catch((e: Error) => setError(e.message));
  };
  useKeys({ Escape: onClose, a: () => pick("approve"), c: () => pick("changes"), m: () => pick("comment") }, { modal: true });
  const counts = noteKinds.flatMap((k) => {
    const n = draft?.comments.filter((c) => c.kind === k).length ?? 0;
    return n ? [`${n} ${k}`] : [];
  });
  const choices: [Verdict, string, string][] = [
    ["approve", "a", "Approve"],
    ["changes", "c", "Request changes"],
    ["comment", "m", "Comment"],
  ];
  return (
    <div className="overlay" onClick={onClose}>
      <div className="dialog submit" onClick={(e) => e.stopPropagation()}>
        <h2>Submit the review</h2>
        <p className="dim">
          {draft
            ? `${draft.comments.length} ${draft.comments.length === 1 ? "comment" : "comments"}${counts.length ? ` (${counts.join(", ")})` : ""}.`
            : "No comments."}{" "}
          Once submitted, crew read it and the orchestrator decides what follows.
        </p>
        <div className="verdicts">
          {choices.map(([v, key, label]) => (
            <button key={v} className={`verdict v-${v}${verdict === v ? " on" : ""}`} onClick={() => pick(v)}>
              <kbd>{key}</kbd> {label}
            </button>
          ))}
        </div>
        <textarea
          ref={area}
          value={summary}
          rows={4}
          placeholder="A summary, if it helps (optional)"
          onChange={(e) => setSummary(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              submit();
            } else if (e.key === "Escape") {
              e.preventDefault();
              onClose();
            }
          }}
        />
        {error && <div className="banner">{error}</div>}
        <div className="note-actions">
          {draft && <button onClick={onDiscard}>Discard the draft</button>}
          <span className="spacer-left" />
          <button onClick={onClose}>
            Cancel <kbd>esc</kbd>
          </button>
          <button className="primary" onClick={submit} disabled={!verdict}>
            Submit <kbd>⌘⏎</kbd>
          </button>
        </div>
      </div>
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
