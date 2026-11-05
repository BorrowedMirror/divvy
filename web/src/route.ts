/** Which view a window renders, derived from the URL hash set in tauri.conf.json. */
export type Route = "overlay" | "settings" | "diagnostics";

export function routeFromHash(hash: string): Route {
  const r = hash.replace(/^#\/?/, "").split("?")[0];
  if (r === "overlay" || r === "diagnostics") return r;
  return "settings";
}
