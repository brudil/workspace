import { useEffect, useState } from "react";

// Mirrors internal/web/api.go.

export interface Workspace {
  name: string;
  root: string;
  missions: Mission[];
}

export interface Mission {
  slug: string;
  title: string;
  state: MissionState;
  created: string;
  updated: string;
  flag?: string;
  capsules: Capsule[];
  crew: Crew[];
}

export type MissionState = "briefing" | "draft" | "active" | "review" | "landed" | "parked" | "archived";

export interface Capsule {
  repo: string;
  name: string;
  branch?: string;
  path: string;
  exists: boolean;
  head?: string;
}

export interface Crew {
  id: string;
  harness: string;
  role?: string;
  capsule?: string;
  started: string;
  ended: boolean;
  activity?: "working" | "waiting" | "permission";
  status?: string;
  lastReply?: string;
}

export class NotSignedIn extends Error {}

export async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: { accept: "application/json" } });
  if (res.status === 401) throw new NotSignedIn();
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `${res.status} ${res.statusText}`);
  }
  return res.json() as Promise<T>;
}

export type Connection = "connecting" | "live" | "lost";

export interface WorkspaceState {
  workspace?: Workspace;
  error?: Error;
  connection: Connection;
}

// useWorkspace keeps the workspace current: it fetches when the event stream
// opens (so nothing that changed while it was closed is missed) and again
// whenever the server says it changed. It never polls.
export function useWorkspace(): WorkspaceState {
  const [state, setState] = useState<WorkspaceState>({ connection: "connecting" });

  useEffect(() => {
    let stopped = false;
    const load = () =>
      getJSON<Workspace>("/api/workspace").then(
        (workspace) => !stopped && setState((s) => ({ ...s, workspace, error: undefined })),
        (error: Error) => !stopped && setState((s) => ({ ...s, error })),
      );
    const events = new EventSource("/api/events");
    events.addEventListener("hello", () => {
      setState((s) => ({ ...s, connection: "live" }));
      load();
    });
    events.addEventListener("workspace", () => load());
    events.onerror = () => {
      setState((s) => ({ ...s, connection: "lost" }));
      // A stream refused for being signed out never opens: find out why.
      if (events.readyState === EventSource.CLOSED) load();
    };
    return () => {
      stopped = true;
      events.close();
    };
  }, []);

  return state;
}

// Review: mirrors internal/review/diff.go.

export interface Diff {
  baseRef: string;
  base: string;
  head: string;
  commits: Commit[];
  uncommitted: string[];
  files: DiffFile[];
}

export interface Commit {
  sha: string;
  subject: string;
  author: string;
  time: string;
}

export interface DiffFile {
  path: string;
  oldPath?: string;
  status: "added" | "deleted" | "modified" | "renamed" | "copied" | "type";
  oldBlob?: string;
  newBlob?: string;
  binary?: boolean;
  additions: number;
  deletions: number;
  large?: boolean;
  hunks: Hunk[];
}

export interface Hunk {
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  section?: string;
  lines: DiffLine[];
}

export interface DiffLine {
  k: "c" | "a" | "d";
  o?: number;
  n?: number;
  t: string;
  noeol?: boolean;
}

export function capsuleAPI(slug: string, repo: string, name: string): string {
  return `/api/missions/${encodeURIComponent(slug)}/capsules/${encodeURIComponent(repo)}/${encodeURIComponent(name)}`;
}

export const getDiff = (api: string) => getJSON<Diff>(`${api}/diff`);

export const getFileDiff = (api: string, base: string, head: string, path: string) =>
  getJSON<DiffFile>(`${api}/file-diff?${new URLSearchParams({ base, head, path })}`);

export const getBlob = (api: string, rev: string, path: string) =>
  getJSON<{ lines: string[] }>(`${api}/blob?${new URLSearchParams({ rev, path })}`).then((b) => b.lines);

// Reviews: mirrors internal/review/review.go.

export const noteKinds = ["blocking", "suggestion", "question", "nit"] as const;
export type NoteKind = (typeof noteKinds)[number];
export type Verdict = "approve" | "changes" | "comment";

export interface ReviewNote {
  id: string;
  path: string;
  side: "new" | "old";
  line: number;
  endLine: number;
  commit: string;
  excerpt: string[];
  kind: NoteKind;
  body: string;
  suggestion?: string | null;
  status: string;
  thread: { by: string; at: string; body: string; commit?: string; status?: string }[];
}

export interface Review {
  id: string;
  capsule: string;
  baseRef: string;
  base: string;
  head: string;
  state: "draft" | "submitted";
  verdict?: Verdict;
  summary?: string;
  created: string;
  submitted?: string;
  comments: ReviewNote[];
}

export interface ReviewState {
  draft: Review | null;
  reviews: Review[];
  viewed: Record<string, string>;
}

async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { "content-type": "application/json", accept: "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  if (res.status === 401) throw new NotSignedIn();
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error ?? `${res.status} ${res.statusText}`);
  return data as T;
}

export interface NoteDraft {
  kind: NoteKind;
  body: string;
  suggestion?: string | null;
}

export const getReview = (api: string) => getJSON<ReviewState>(`${api}/review`);

export const addNote = (api: string, at: { base: string; head: string }, n: NoteDraft & { path: string; side: "new" | "old"; line: number; endLine: number }) =>
  send<ReviewState>("POST", `${api}/review/comments`, { ...at, ...n });

export const editNote = (api: string, id: string, n: NoteDraft) =>
  send<ReviewState>("PATCH", `${api}/review/comments/${encodeURIComponent(id)}`, n);

export const deleteNote = (api: string, id: string) => send<ReviewState>("DELETE", `${api}/review/comments/${encodeURIComponent(id)}`);

export const setViewed = (api: string, path: string, blob: string, viewed: boolean) =>
  send<ReviewState>("PUT", `${api}/review/viewed`, { path, blob, viewed });

export const submitReview = (api: string, at: { base: string; head: string }, verdict: Verdict, summary: string) =>
  send<ReviewState>("POST", `${api}/review/submit`, { ...at, verdict, summary });

export const discardReview = (api: string) => send<ReviewState>("DELETE", `${api}/review`);

// Missions in full: mirrors MissionDetail in internal/web/api.go.

export interface Proposal {
  text: string;
  by?: string;
  capsule?: string;
  options?: string[];
}

export interface Stage {
  n: number;
  title: string; // a few words
  text: string; // the whole stage
  state: "accepted" | "current" | "todo";
  crew?: string[];
  checkIn?: boolean;
  why?: string;
}

export interface CapsuleDetail extends Capsule {
  dirty?: boolean;
  ahead?: number;
  behind?: number;
  pr?: { number: number; title: string; url: string; merged?: boolean; checks?: string };
}

export interface CrewDetail extends Crew {
  endedAt?: string;
  exit?: string;
  note?: string;
  dispatch?: string;
  handoff?: string;
}

export interface Artifact {
  ref: string;
  kind: string;
  label?: string;
  state: string;
  url?: string;
  path?: string;
  by?: string;
  added: string;
  readable: boolean;
}

export interface MissionEvent {
  time: string;
  kind: string;
  text: string;
}

export interface MissionDetail {
  slug: string;
  title: string;
  state: MissionState;
  created: string;
  request?: string;
  from?: string;
  brief: string;
  flag?: Proposal;
  dispatch?: Proposal;
  conclude?: Proposal;
  stages: Stage[];
  capsules: CapsuleDetail[];
  crew: CrewDetail[];
  artifacts: Artifact[];
  events: MissionEvent[];
  github: boolean;
}

export interface ArtifactContent extends Artifact {
  text: string;
  markdown: boolean;
}

export const getMission = (slug: string, github: boolean) =>
  getJSON<MissionDetail>(`/api/missions/${encodeURIComponent(slug)}${github ? "?github=1" : ""}`);

export const getArtifact = (slug: string, ref: string) =>
  getJSON<ArtifactContent>(`/api/missions/${encodeURIComponent(slug)}/artifact?${new URLSearchParams({ ref })}`);
