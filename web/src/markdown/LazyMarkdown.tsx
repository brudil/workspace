import { lazy, Suspense } from "react";

// The markdown renderer loads with the first page that shows markdown, not
// with every page.
const Inner = lazy(() => import("./Markdown").then((m) => ({ default: m.Markdown })));

export function Markdown({ text }: { text: string }) {
  return (
    <Suspense fallback={<div className="markdown markdown-loading">{text}</div>}>
      <Inner text={text} />
    </Suspense>
  );
}
