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

async function getJSON<T>(path: string): Promise<T> {
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
