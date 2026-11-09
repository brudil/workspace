import type { DiffFile, DiffLine, Hunk } from "../api";

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
  | { type: "note"; key: string; file: number; text: string; large?: boolean };

// The cursor stops on these.
export const navigable = (r: Row) => r.type !== "note" || !!r.large;

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

// fileRows lays out one file.
export function fileRows(file: number, f: DiffFile, s: FileState, split: boolean): Row[] {
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
