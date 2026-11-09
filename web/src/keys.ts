import { useEffect, useRef } from "react";

export type KeyMap = Record<string, (e: KeyboardEvent) => void>;

// keyName is how a key press is named in a KeyMap: "j", "J", "Enter",
// "Escape", "mod+d" (⌘ on macOS, ctrl elsewhere), "shift+ArrowDown". Shift
// is named only for keys whose name it doesn't change already.
export function keyName(e: KeyboardEvent): string {
  const mod = e.metaKey || e.ctrlKey ? "mod+" : "";
  const shift = e.shiftKey && e.key.length > 1 ? "shift+" : "";
  return mod + shift + e.key;
}

function typing(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
}

// While a modal (help, a finder) is open, only its keys work.
let modals = 0;

// useKeys binds keys for as long as the component is mounted. Keys typed
// into a field are the field's, apart from Escape. A modal's keys work
// while it's open, and nobody else's do.
export function useKeys(keys: KeyMap, opts: { modal?: boolean } = {}) {
  const current = useRef(keys);
  current.current = keys;
  const modal = !!opts.modal;
  useEffect(() => {
    if (modal) modals++;
    const onKey = (e: KeyboardEvent) => {
      // Handled already (an editor's own Escape, say, which may have closed
      // it, and with it the modal that kept this one quiet).
      if (e.defaultPrevented) return;
      if (e.isComposing || (typing(e.target) && e.key !== "Escape")) return;
      if (!modal && modals > 0) return;
      const fn = current.current[keyName(e)];
      if (fn) {
        e.preventDefault();
        fn(e);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      if (modal) modals--;
      window.removeEventListener("keydown", onKey);
    };
  }, [modal]);
}
