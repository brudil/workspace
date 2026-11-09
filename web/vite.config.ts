import { readFileSync } from "node:fs";
import { join } from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// `just web-dev <workspace>` runs this dev server against that workspace's
// running web UI (ws web starts it): the API is proxied there, signed in
// with the workspace's token, so the page here needs no sign-in.
function devTarget(): { origin: string; token: string } | undefined {
  const root = process.env.WS_ROOT;
  if (!root) return undefined;
  try {
    const info = JSON.parse(readFileSync(join(root, ".ws", "web.json"), "utf8"));
    if (info.port) return { origin: `http://127.0.0.1:${info.port}`, token: info.token };
  } catch {
    // no web UI running there yet
  }
  return undefined;
}

export default defineConfig(() => {
  const target = devTarget();
  return {
    plugins: [react()],
    build: { outDir: "../internal/web/dist", emptyOutDir: true },
    server: {
      port: 5173,
      strictPort: true,
      proxy: target && {
        "/api": {
          target: target.origin,
          changeOrigin: true,
          headers: { origin: target.origin, authorization: `Bearer ${target.token}` },
        },
      },
    },
  };
});
