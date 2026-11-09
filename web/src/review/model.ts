import type { DiffFile, DiffLine, Hunk, ReviewNote } from "../api";

// How a diff is laid out as rows: one header per file, its lines (unified,
// or old and new side by side), and gaps between hunks that open up to show
// the unchanged lines, read from the file at head.

export type Kind = "c" | "a" | "d";

export interface Cell {
  num: number;
  text: string;
  kind: Kind;
  noeol?: boolean;
  // shown is an unchanged line opened up from a gap, not one of the diff's;
  // at is its line in the file at head.
  shown?: boolean;
  at?: number;
}

export type Row =
  | { type: "file"; key: string; file: number }
  | { type: "gap"; key: string; file: number; gap: number; count?: number; section?: string }
  | { type: "line"; key: string; file: number; left?: Cell; right?: Cell }
  | { type: "note"; key: string; file: number; text: string; large?: boolean }
  | { type: "comment"; key: string; file: number; placed: Placed; unplaced?: boolean }
  | { type: "editor"; key: string; file: number };

// The cursor stops on these (the editor has the keyboard to itself).
export const navigable = (r: Row) => (r.type !== "note" || !!r.large) && r.type !== "editor";

// Placed is a comment to show in the diff: one of the draft's, or of a
// submitted review (read only). Stale ones were made on another commit,
// so their lines may have moved.
export interface Placed {
  note: ReviewNote;
  review: string;
  draft: boolean;
  stale: boolean;
}

// Target is the lines a comment is on.
export interface Target {
  side: "new" | "old";
  line: number;
  endLine: number;
}

// Editing is where the comment editor is open: on new lines, or on a
// draft comment it's rewriting.
export interface Editing {
  file: number;
  target: Target;
  noteID?: string;
  note?: ReviewNote; // the draft comment being rewritten
}

// anchors: a comment goes after the last line it's on. Old-side comments
// are on removed lines.
export const anchors = (r: Row, t: Target) =>
  r.type === "line" && (t.side === "new" ? r.right?.num === t.endLine : r.left?.kind === "d" && r.left.num === t.endLine);

export const isChange = (r: Row) =>
  r.type === "line" && (r.left?.kind === "d" || r.left?.kind === "a" || r.right?.kind === "a" || r.right?.kind === "d");

// Expansion is how many of a gap's lines are shown at its top (after the
// hunk above) and its bottom (before the hunk below).
export interface Expansion {
  top: number;
  bottom: number;
}

export interface FileState {
  viewed: boolean;
  blob?: string[]; // the file at head, once loaded
  expanded: Record<number, Expansion>;
}

// Gap is the new-side lines between hunks; their old-side numbers are
// offset by a constant.
export interface Gap {
  start: number;
  end?: number; // unknown until the file is loaded (after the last hunk)
  offset: number;
}

const firstNew = (h: Hunk) => (h.newLines === 0 ? h.newStart + 1 : h.newStart);
const firstOld = (h: Hunk) => (h.oldLines === 0 ? h.oldStart + 1 : h.oldStart);
const afterNew = (h: Hunk) => firstNew(h) + h.newLines;
const afterOld = (h: Hunk) => firstOld(h) + h.oldLines;

// canExpand: only a file with both sides has unchanged lines to show.
export const canExpand = (f: DiffFile) =>
  f.hunks.length > 0 && !f.binary && f.status !== "added" && f.status !== "deleted";

// gaps are the hunks' surroundings: before each hunk, and after the last.
export function gaps(f: DiffFile, blob?: string[]): Gap[] {
  const hs = f.hunks;
  const out: Gap[] = hs.map((h, i) => ({
    start: i === 0 ? 1 : afterNew(hs[i - 1]),
    end: firstNew(h) - 1,
    offset: firstOld(h) - firstNew(h),
  }));
  const last = hs[hs.length - 1];
  out.push({ start: afterNew(last), end: blob ? blob.length : undefined, offset: afterOld(last) - afterNew(last) });
  return out;
}

const lineKey = (file: number, l?: Cell, r?: Cell) => (r ? `${file}:n${r.num}` : `${file}:o${l!.num}`);

function cell(l: DiffLine, side: "old" | "new"): Cell {
  return { num: (side === "old" ? l.o : l.n) ?? 0, text: l.t, kind: l.k, noeol: l.noeol };
}

function hunkRows(file: number, h: Hunk, split: boolean): Row[] {
  const rows: Row[] = [];
  const push = (left?: Cell, right?: Cell) => rows.push({ type: "line", key: lineKey(file, left, right), file, left, right });
  if (!split) {
    for (const l of h.lines) {
      if (l.k === "c") push(cell(l, "old"), cell(l, "new"));
      else if (l.k === "d") push(cell(l, "old"));
      else push(undefined, cell(l, "new"));
    }
    return rows;
  }
  // Side by side: a run of deletions pairs off with the additions after it.
  let dels: Cell[] = [];
  let adds: Cell[] = [];
  const flush = () => {
    for (let i = 0; i < Math.max(dels.length, adds.length); i++) push(dels[i], adds[i]);
    dels = [];
    adds = [];
  };
  for (const l of h.lines) {
    if (l.k === "d") {
      if (adds.length) flush();
      dels.push(cell(l, "old"));
    } else if (l.k === "a") adds.push(cell(l, "new"));
    else {
      flush();
      push(cell(l, "old"), cell(l, "new"));
    }
  }
  flush();
  return rows;
}

function shownRow(file: number, g: Gap, n: number, blob: string[]): Row {
  const text = blob[n - 1] ?? "";
  const left: Cell = { num: n + g.offset, text, kind: "c", shown: true, at: n };
  const right: Cell = { num: n, text, kind: "c", shown: true, at: n };
  return { type: "line", key: lineKey(file, left, right), file, left, right };
}

function gapRows(file: number, gi: number, g: Gap, s: FileState, section?: string): Row[] {
  const e = s.expanded[gi] ?? { top: 0, bottom: 0 };
  if (g.end === undefined) {
    return [{ type: "gap", key: `${file}:g${gi}`, file, gap: gi, section }];
  }
  const count = g.end - g.start + 1;
  if (count <= 0) return [];
  const top = Math.min(e.top, count);
  const bottom = Math.min(e.bottom, count - top);
  const rows: Row[] = [];
  for (let n = g.start; n < g.start + top; n++) rows.push(shownRow(file, g, n, s.blob!));
  if (count - top - bottom > 0) rows.push({ type: "gap", key: `${file}:g${gi}`, file, gap: gi, count: count - top - bottom, section });
  for (let n = g.end - bottom + 1; n <= g.end; n++) rows.push(shownRow(file, g, n, s.blob!));
  return rows;
}

// fileRows lays out one file, with its comments and the editor where
// they're anchored.
export function fileRows(file: number, f: DiffFile, s: FileState, split: boolean, notes: Placed[] = [], editing?: Editing): Row[] {
  const rows = diffRows(file, f, s, split);
  if (s.viewed || (!notes.length && !editing)) return rows;
  const out: Row[] = [];
  const placed = new Set<string>();
  const editorRow: Row = { type: "editor", key: `${file}:editor`, file };
  for (const r of rows) {
    out.push(r);
    if (r.type !== "line") continue;
    for (const p of notes) {
      if (placed.has(p.note.id) || !anchors(r, p.note)) continue;
      placed.add(p.note.id);
      out.push(editing?.noteID === p.note.id ? editorRow : { type: "comment", key: `${file}:c:${p.note.id}`, file, placed: p });
    }
    if (editing && !editing.noteID && anchors(r, editing.target)) out.push(editorRow);
  }
  // Comments whose lines aren't showing (in a closed gap, or moved since)
  // go at the end of the file.
  for (const p of notes) {
    if (placed.has(p.note.id)) continue;
    out.push(editing?.noteID === p.note.id ? editorRow : { type: "comment", key: `${file}:c:${p.note.id}`, file, placed: p, unplaced: true });
  }
  return out;
}

function diffRows(file: number, f: DiffFile, s: FileState, split: boolean): Row[] {
  const rows: Row[] = [{ type: "file", key: `${file}`, file }];
  if (s.viewed) return rows;
  const note = (text: string, large?: boolean) => rows.push({ type: "note", key: `${file}:note`, file, text, large });
  if (f.large) {
    note(`Large diff: ${f.additions + f.deletions} changed lines, hidden. ⏎ shows it.`, true);
    return rows;
  }
  if (f.binary) {
    note("Binary file.");
    return rows;
  }
  if (!f.hunks.length) {
    note(f.status === "renamed" ? `Renamed from ${f.oldPath}, unchanged.` : f.status === "added" ? "Empty file." : "No changes to show.");
    return rows;
  }
  const gs = canExpand(f) ? gaps(f, s.blob) : [];
  f.hunks.forEach((h, hi) => {
    if (gs.length) rows.push(...gapRows(file, hi, gs[hi], s, h.section));
    rows.push(...hunkRows(file, h, split));
  });
  if (gs.length) rows.push(...gapRows(file, f.hunks.length, gs[f.hunks.length], s));
  return rows;
}

// sameLine finds the row in rows showing what row did (for keeping the
// cursor still when the layout changes).
export function sameLine(rows: Row[], row: Row): Row | undefined {
  if (row.type !== "line") return rows.find((r) => r.key === row.key);
  return rows.find(
    (r) =>
      r.type === "line" &&
      r.file === row.file &&
      ((row.right && r.right?.num === row.right.num) || (!row.right && row.left && r.left?.num === row.left.num)),
  );
}
