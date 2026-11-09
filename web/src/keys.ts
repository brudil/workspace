import { useEffect, useRef } from "react";

export type KeyMap = Record<string, (e: KeyboardEvent) => void>;

// keyName is how a key press is named in a KeyMap: "j", "J", "Enter",
// "Escape", "mod+k" (⌘ on macOS, ctrl elsewhere).
export function keyName(e: KeyboardEvent): string {
  const mod = e.metaKey || e.ctrlKey ? "mod+" : "";
  return mod + e.key;
}

function typing(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
}

// useKeys binds keys for as long as the component is mounted. Keys typed
// into a field are the field's, apart from Escape.
export function useKeys(keys: KeyMap) {
  const current = useRef(keys);
  current.current = keys;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.isComposing || (typing(e.target) && e.key !== "Escape")) return;
      const fn = current.current[keyName(e)];
      if (fn) {
        e.preventDefault();
        fn(e);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}
