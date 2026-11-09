import type { HLJSApi } from "highlight.js";

// Syntax highlighting, loaded only when a review is open: until it arrives,
// code shows plain.

let hljs: HLJSApi | undefined;
let loading: Promise<HLJSApi> | undefined;

export function loadHighlighter(): Promise<HLJSApi> {
  loading ??= import("highlight.js/lib/common").then((m) => (hljs = m.default));
  return loading;
}

const byExtension: Record<string, string> = {
  go: "go",
  ts: "typescript", tsx: "typescript", mts: "typescript", cts: "typescript",
  js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript",
  json: "json", md: "markdown", markdown: "markdown",
  py: "python", rb: "ruby", rs: "rust", java: "java", kt: "kotlin", kts: "kotlin", swift: "swift",
  c: "c", h: "c", cc: "cpp", cpp: "cpp", cxx: "cpp", hpp: "cpp", hh: "cpp", cs: "csharp",
  css: "css", scss: "scss", less: "less", html: "xml", htm: "xml", xml: "xml", svg: "xml",
  yaml: "yaml", yml: "yaml", toml: "ini", ini: "ini", cfg: "ini",
  sh: "bash", bash: "bash", zsh: "bash", sql: "sql", php: "php", lua: "lua", pl: "perl", r: "r",
  graphql: "graphql", gql: "graphql", diff: "diff", patch: "diff",
};
const byName: Record<string, string> = { Makefile: "makefile", justfile: "makefile", "go.mod": "go" };

// languageOf is the language a path is highlighted as, if any.
export function languageOf(path: string): string | undefined {
  const base = path.split("/").pop() ?? "";
  const ext = base.includes(".") ? base.slice(base.lastIndexOf(".") + 1).toLowerCase() : "";
  const lang = byName[base] ?? byExtension[ext];
  return lang && hljs?.getLanguage(lang) ? lang : undefined;
}

export function escapeHTML(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

// highlightLines highlights lines together (so a string or comment spanning
// lines reads right) and hands back each line's HTML.
export function highlightLines(lines: string[], lang: string | undefined): string[] {
  if (!hljs || !lang || !lines.length) return lines.map(escapeHTML);
  const html = hljs.highlight(lines.join("\n"), { language: lang, ignoreIllegals: true }).value;
  const out = splitLines(html);
  return out.length === lines.length ? out : lines.map(escapeHTML);
}

// splitLines splits highlighted HTML at newlines, closing the spans open at
// each line's end and reopening them on the next.
function splitLines(html: string): string[] {
  const out: string[] = [];
  const open: string[] = [];
  let cur = "";
  for (const m of html.matchAll(/<span[^>]*>|<\/span>|\n|[^<\n]+|</g)) {
    const t = m[0];
    if (t === "\n") {
      out.push(cur + "</span>".repeat(open.length));
      cur = open.join("");
    } else if (t.startsWith("<span")) {
      open.push(t);
      cur += t;
    } else if (t === "</span>") {
      open.pop();
      cur += t;
    } else {
      cur += t;
    }
  }
  out.push(cur);
  return out;
}

// highlightCode colours a fenced block by its language name, if known.
export function highlightCode(code: string, lang: string | undefined): string {
  if (!hljs || !lang || !hljs.getLanguage(lang)) return escapeHTML(code);
  return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
}
