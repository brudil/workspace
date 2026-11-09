import { type ReactElement, type ReactNode, useEffect, useId, useState } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { highlightCode, loadHighlighter } from "../review/highlight";

// Markdown as the deck's artifacts, briefs, handoffs and reviews are
// written: GitHub-flavoured (tables, task lists, strikethrough), code
// highlighted, and ```mermaid blocks drawn as diagrams.
//
// What's rendered comes from agents and repos, so raw HTML stays off
// (react-markdown's default) and links keep to safe schemes: this page can
// make writes as you.

export function Markdown({ text }: { text: string }) {
  const [, setReady] = useState(false);
  useEffect(() => {
    loadHighlighter().then(() => setReady(true));
  }, []);
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  );
}

const components: Components = {
  pre: ({ children }) => <Block>{children}</Block>,
  a: ({ href, children }) => {
    const external = !!href && /^[a-z]+:/i.test(href);
    return (
      <a href={href} {...(external ? { target: "_blank", rel: "noopener noreferrer" } : {})}>
        {children}
      </a>
    );
  },
  // A task list's boxes show, but can't be ticked here.
  input: ({ type, checked }) => (type === "checkbox" ? <input type="checkbox" checked={!!checked} readOnly disabled /> : null),
};

// Block is a fenced code block: a diagram for mermaid, else code.
function Block({ children }: { children?: ReactNode }) {
  const code = children as ReactElement<{ className?: string; children?: ReactNode }> | undefined;
  const lang = /language-([\w+-]+)/.exec(code?.props?.className ?? "")?.[1];
  const text = String(code?.props?.children ?? "").replace(/\n$/, "");
  if (lang === "mermaid") return <Mermaid code={text} />;
  return (
    <pre className="code-block">
      {lang && <span className="code-lang">{lang}</span>}
      <code dangerouslySetInnerHTML={{ __html: highlightCode(text, lang) }} />
    </pre>
  );
}

type MermaidAPI = typeof import("mermaid").default;
let mermaidLoading: Promise<MermaidAPI> | undefined;

// loadMermaid loads Mermaid the first time a diagram needs it (it's large).
function loadMermaid(): Promise<MermaidAPI> {
  mermaidLoading ??= import("mermaid").then(({ default: mermaid }) => {
    const dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    // strict: no scripts or click handlers from the diagram's text.
    mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: dark ? "dark" : "default", fontFamily: "inherit" });
    return mermaid;
  });
  return mermaidLoading;
}

function Mermaid({ code }: { code: string }) {
  const id = "mermaid-" + useId().replace(/[^a-zA-Z0-9-]/g, "");
  const [svg, setSvg] = useState<string>();
  const [error, setError] = useState<string>();
  useEffect(() => {
    let live = true;
    loadMermaid()
      .then((mermaid) => mermaid.render(id, code))
      .then(
        (r) => live && setSvg(r.svg),
        (e: Error) => live && setError(e?.message ?? String(e)),
      )
      .finally(() => document.getElementById("d" + id)?.remove()); // what a failed render leaves behind
    return () => {
      live = false;
    };
  }, [code, id]);
  if (error) {
    return (
      <div className="mermaid-error">
        <div>This diagram couldn't be drawn: {error.split("\n")[0]}</div>
        <pre className="code-block">
          <code>{code}</code>
        </pre>
      </div>
    );
  }
  if (!svg) return <pre className="code-block mermaid-loading">{code}</pre>;
  return <div className="mermaid" dangerouslySetInnerHTML={{ __html: svg }} />;
}
