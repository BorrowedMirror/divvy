// Screenshots of every view for the docs, from the browser preview (src/devMock.ts): starts Vite,
// drives a headless Chromium over the DevTools protocol, and writes docs/images/<name>.png at 2x.
// Text is set in Noto Sans and Noto Sans Mono (the @fontsource dev dependencies), a typical
// Linux desktop's system fonts, so the images match whatever machine takes them.
//
//   make screenshots                    (from the repository root)
//   node web/scripts/screenshots.mjs [name...]   only the named screenshots
//
// Chromium is found on PATH (chromium, chromium-browser, google-chrome, google-chrome-stable) or
// named with CHROME=/path/to/chrome.
import { spawn, spawnSync } from "node:child_process";
import { once } from "node:events";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const web = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const out = resolve(web, "..", "docs", "images");

// Settings-sized views are cropped to their content column (max-w-2xl, 672 px).
const column = { width: 672, height: 1600, clip: "main" };
const tab = (name) =>
  `[...document.querySelectorAll('[role=tab]')].find((t) => t.textContent.trim() === ${JSON.stringify(name)}).click()`;
const emit = (event, payload) => `window.divvyPreview.emit(${JSON.stringify(event)}, ${JSON.stringify(payload)})`;

const shots = [
  {
    name: "price-check",
    route: "overlay",
    width: 420,
    height: 1600,
    clip: 'article[aria-label="Price check"]',
    ready: '[data-testid="price-median"]',
  },
  ...["General", "Hotkeys", "In game", "Trade", "Overlay", "Waystones", "Platform"].map((t) => ({
    name: `settings-${t.toLowerCase().replace(/ /g, "-")}`,
    route: "settings",
    ...column,
    ready: '[data-testid="settings-root"] [role=tab]',
    setup: tab(t),
  })),
  {
    name: "update",
    route: "settings",
    ...column,
    ready: '[data-testid="settings-root"] [role=tab]',
    setup: emit("update-available", { version: "0.2.0", notes: null, date: null }),
    wait: "main [role=status]",
  },
  // The preview's ?platform= picks what Diagnostics and Setup describe (src/devMock.ts).
  ...["macos", "linux", "windows"].flatMap((p) => [
    { name: `diagnostics-${p}`, route: `diagnostics?platform=${p}`, ...column, ready: "main h1" },
    { name: `onboarding-${p}`, route: `onboarding?platform=${p}`, ...column, ready: "main h1" },
  ]),
  {
    name: "ready-notice",
    route: "notice",
    width: 300,
    height: 64,
    clip: '[data-testid="ready-notice"]',
    ready: "main",
    setup: emit("ready-notice", { duration_ms: 600_000 }),
    wait: '[data-testid="ready-notice"]',
  },
];

// The README's grid, two views to a row: both are framed at the taller one's height, so the
// table's cells match. The shorter view shows its window's background below it.
const readmeRows = [
  ["settings-hotkeys", "update"],
  ["settings-trade", "settings-overlay"],
  ["diagnostics-linux", "settings-in-game"],
];
readmeRows.forEach((row, i) => {
  for (const name of row) {
    shots.push({ ...shots.find((s) => s.name === name), name: `readme-${name.replace(/^settings-/, "")}`, row: i });
  }
});

// The @fontsource CSS (every subset, with its unicode-range), its files inlined as data URIs.
function fontCSS() {
  const css = [];
  for (const [pkg, weights] of [
    ["noto-sans", [400, 500, 600, 700]],
    ["noto-sans-mono", [400, 500]],
  ]) {
    const dir = join(web, "node_modules", "@fontsource", pkg);
    for (const w of weights) {
      css.push(
        readFileSync(join(dir, `${w}.css`), "utf8").replace(/url\(\.\/files\/([^)]+)\)/g, (_, f) => {
          const type = f.endsWith(".woff2") ? "font/woff2" : "font/woff";
          return `url(data:${type};base64,${readFileSync(join(dir, "files", f)).toString("base64")})`;
        }),
      );
    }
  }
  return `${css.join("\n")}:root{--font-sans:"Noto Sans",sans-serif;--font-mono:"Noto Sans Mono",monospace}.min-h-screen{min-height:0!important}`;
}

function findChrome() {
  for (const name of [process.env.CHROME, "chromium", "chromium-browser", "google-chrome", "google-chrome-stable"]) {
    if (name && spawnSync(name, ["--version"], { stdio: "ignore" }).status === 0) return name;
  }
  throw new Error("Chromium not found: install it, or set CHROME=/path/to/chrome");
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function browser() {
  const profile = mkdtempSync(join(tmpdir(), "divvy-shots-"));
  const port = 9222 + Math.floor(Math.random() * 1000);
  const proc = spawn(
    findChrome(),
    [
      "--headless=new",
      "--no-sandbox",
      "--disable-gpu",
      "--hide-scrollbars",
      `--remote-debugging-port=${port}`,
      `--user-data-dir=${profile}`,
      "about:blank",
    ],
    { stdio: "ignore" },
  );
  let target;
  for (let i = 0; i < 100 && !target; i++) {
    try {
      target = (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find((t) => t.type === "page");
    } catch {
      await sleep(100);
    }
  }
  if (!target) throw new Error("Chromium's DevTools endpoint did not come up");
  const ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((r, j) => {
    ws.addEventListener("open", r, { once: true });
    ws.addEventListener("error", j, { once: true });
  });
  let id = 0;
  const pending = new Map();
  ws.addEventListener("message", (e) => {
    const m = JSON.parse(e.data);
    if (m.id && pending.has(m.id)) {
      pending.get(m.id)(m);
      pending.delete(m.id);
    }
  });
  const send = (method, params = {}) =>
    new Promise((r, j) => {
      const n = ++id;
      pending.set(n, (m) => (m.error ? j(new Error(`${method}: ${m.error.message}`)) : r(m.result)));
      ws.send(JSON.stringify({ id: n, method, params }));
    });
  // Chromium writes its profile until it exits, so wait before removing it.
  const close = async () => {
    ws.close();
    const exited = proc.exitCode === null ? once(proc, "exit") : Promise.resolve();
    proc.kill();
    await exited;
    rmSync(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  };
  return { send, close };
}

async function main() {
  const only = process.argv.slice(2);
  const todo = only.length ? shots.filter((s) => only.includes(s.name)) : shots;
  if (todo.length === 0) throw new Error(`no such screenshot; one of: ${shots.map((s) => s.name).join(", ")}`);
  const fonts = fontCSS();
  const server = await createServer({
    root: web,
    mode: "development",
    logLevel: "error",
    server: { host: "127.0.0.1", port: 0, strictPort: false },
  });
  await server.listen();
  const base = server.resolvedUrls.local[0];
  const { send, close } = await browser();
  const evaluate = async (expression) => {
    const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
    if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description ?? r.exceptionDetails.text);
    return r.result.value;
  };
  const waitFor = async (selector, what) => {
    for (let i = 0; i < 150; i++) {
      if (await evaluate(`!!document.querySelector(${JSON.stringify(selector)})`)) return;
      await sleep(100);
    }
    throw new Error(`${what}: ${selector} never appeared`);
  };
  mkdirSync(out, { recursive: true });
  try {
    await send("Page.enable");
    await send("Emulation.setEmulatedMedia", {
      features: [
        { name: "prefers-color-scheme", value: "dark" },
        { name: "prefers-reduced-motion", value: "reduce" },
      ],
    });
    await send("Emulation.setDefaultBackgroundColorOverride", { color: { r: 0, g: 0, b: 0, a: 0 } });
    // Opens a view as the screenshot shows it, and returns its bounds.
    const open = async (s) => {
      await send("Emulation.setDeviceMetricsOverride", {
        width: s.width,
        height: s.height,
        deviceScaleFactor: 2,
        mobile: false,
      });
      await send("Page.navigate", { url: "about:blank" });
      await send("Page.navigate", { url: `${base}#/${s.route}` });
      await waitFor(s.ready, s.name);
      await evaluate(
        `(() => { const st = document.createElement("style"); st.textContent = ${JSON.stringify(fonts)}; document.head.append(st); return document.fonts.ready.then(() => true); })()`,
      );
      if (s.setup) await evaluate(s.setup);
      if (s.wait) await waitFor(s.wait, s.name);
      await sleep(600); // let queries settle and transitions finish
      await evaluate("document.fonts.ready.then(() => true)");
      return evaluate(
        `(() => { const b = document.querySelector(${JSON.stringify(s.clip)}).getBoundingClientRect(); return [b.x, b.y, b.width, b.height]; })()`,
      );
    };
    const frames = new Map();
    for (const s of todo) {
      if (s.row !== undefined && !frames.has(s.row)) {
        let tallest = 0;
        for (const peer of shots.filter((p) => p.row === s.row)) tallest = Math.max(tallest, (await open(peer))[3]);
        frames.set(s.row, Math.ceil(tallest));
      }
      const r = await open(s);
      if (s.row !== undefined) r[3] = frames.get(s.row);
      const shot = await send("Page.captureScreenshot", {
        format: "png",
        clip: { x: r[0], y: r[1], width: r[2], height: r[3], scale: 1 },
      });
      writeFileSync(join(out, `${s.name}.png`), Buffer.from(shot.data, "base64"));
      console.log(`docs/images/${s.name}.png  ${Math.round(r[2])}x${Math.round(r[3])}`);
    }
  } finally {
    await close();
    await server.close();
  }
  const stale = readdirSync(out).filter((f) => f.endsWith(".png") && !shots.some((s) => `${s.name}.png` === f));
  if (stale.length) console.log(`not made by this script (left alone): ${stale.join(", ")}`);
}

main().catch((e) => {
  console.error(`screenshots: ${e.message}`);
  process.exit(1);
});
