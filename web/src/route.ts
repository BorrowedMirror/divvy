/** Which view a window renders, from the URL hash the shell gives each window. */
export type Route = "overlay" | "settings" | "diagnostics" | "onboarding" | "notice";

export function routeFromHash(hash: string): Route {
  const r = hash.replace(/^#\/?/, "").split("?")[0];
  if (r === "overlay" || r === "diagnostics" || r === "onboarding" || r === "notice") return r;
  return "settings";
}
