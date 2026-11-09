import { useCallback, useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router";
import { type ArtifactContent, getArtifact, type Workspace } from "../api";
import { useKeys } from "../keys";
import { Markdown } from "../markdown/LazyMarkdown";
import { highlightCode, languageOf, loadHighlighter } from "../review/highlight";

export const artifactKeys: [string, string][] = [
  ["j / k", "Scroll down / up"],
  ["⌘D / ⌘U", "Down / up half a page"],
  ["g / G", "Top / bottom"],
  ["o", "Open it on GitHub (or where it lives)"],
  ["r", "Reload"],
  ["esc", "Back"],
];

// ArtifactPage shows one of a mission's artifacts: markdown rendered, with
// diagrams; anything else as code.
export function ArtifactPage({ workspace }: { workspace: Workspace }) {
  const { slug = "" } = useParams();
  const [params] = useSearchParams();
  const ref = params.get("ref") ?? "";
  const navigate = useNavigate();
  const location = useLocation();
  const mission = workspace.missions.find((m) => m.slug === slug);
  const [a, setA] = useState<ArtifactContent>();
  const [error, setError] = useState<string>();
  const [, setReady] = useState(false);

  const load = useCallback(() => {
    getArtifact(slug, ref).then(
      (x) => {
        setA(x);
        setError(undefined);
      },
      (e: Error) => setError(e.message),
    );
  }, [slug, ref]);
  useEffect(load, [load]);
  useEffect(() => {
    loadHighlighter().then(() => setReady(true));
  }, []);

  const scroller = () => document.querySelector("main");
  const scroll = (by: number) => scroller()?.scrollBy({ top: by });
  // Back where you came from, or to the mission's work if this was opened
  // directly.
  const back = () => (location.key !== "default" ? navigate(-1) : navigate(`/missions/${slug}?tab=work`));
  useKeys({
    j: () => scroll(60),
    ArrowDown: () => scroll(60),
    k: () => scroll(-60),
    ArrowUp: () => scroll(-60),
    "mod+d": () => scroll(window.innerHeight / 2),
    "mod+u": () => scroll(-window.innerHeight / 2),
    g: () => scroller()?.scrollTo({ top: 0 }),
    G: () => scroller()?.scrollTo({ top: scroller()!.scrollHeight }),
    o: () => a?.url && window.open(a.url, "_blank", "noopener"),
    r: load,
    Escape: back,
    h: back,
  });

  return (
    <div className="page artifact-page">
      <header className="page-head">
        <Link to="/" className="dim">
          Missions
        </Link>
        <span className="dim">/</span>
        <Link to={`/missions/${slug}?tab=work`} className="dim">
          {mission?.title ?? slug}
        </Link>
        <span className="dim">/</span>
        <h1>{a?.label || ref}</h1>
      </header>
      {a && (
        <div className="artifact-meta">
          <span className="dim">{a.kind}</span>
          <span className="dim">·</span>
          <span>{a.state}</span>
          <span className="dim">·</span>
          <span className="mono dim">{a.ref}</span>
          {a.by && <span className="dim">· recorded by {a.by}</span>}
          <span className="spacer" />
          {a.url && (
            <a href={a.url} target="_blank" rel="noopener noreferrer">
              Open ↗ <kbd>o</kbd>
            </a>
          )}
        </div>
      )}
      {error && <div className="banner">{error}</div>}
      {a &&
        (a.markdown ? (
          <article className="artifact-body">
            <Markdown text={a.text} />
          </article>
        ) : (
          <pre className="code-block artifact-code">
            <code dangerouslySetInnerHTML={{ __html: highlightCode(a.text, languageOf(a.path ?? a.ref)) }} />
          </pre>
        ))}
      {a?.path && <p className="dim mono path">{a.path}</p>}
    </div>
  );
}
