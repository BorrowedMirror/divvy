import { StrictMode } from "react";

import { createRoot } from "react-dom/client";

import { App } from "@src/App";
import "@src/index.css";
import { routeFromHash } from "@src/route";

async function boot() {
  // Plain-browser dev preview: no Tauri runtime, so fake the backend with fixtures.
  if (import.meta.env.DEV && !("__TAURI_INTERNALS__" in window)) {
    const { installDevMock } = await import("@src/devMock");
    installDevMock(routeFromHash(window.location.hash));
  }
  const root = document.getElementById("root");
  if (!root) throw new Error("missing #root");
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

void boot();
