import type { ModFlag } from "@src/bindings";

export type Flags = Partial<Record<string, ModFlag>>;

/** The flag the user set for a waystone mod, by its template (`#` for rolled numbers). */
export function flagFor(template: string, flags: Flags): ModFlag | null {
  return flags[template.trim()] ?? null;
}

/** Flags with `template` set to `flag`, or cleared when it already has that flag (toggle). */
export function toggleFlag(flags: Flags, template: string, flag: ModFlag | null): Record<string, ModFlag> {
  const key = template.trim();
  const clear = flag === null || flags[key] === flag;
  const next: Record<string, ModFlag> = {};
  for (const [k, v] of Object.entries(flags)) if (v && k !== key) next[k] = v;
  if (!clear) next[key] = flag;
  return next;
}

export type Verdict = { kind: "danger" | "warn" | "good" | "clear"; danger: number; warn: number; good: number };

/** Overall verdict for a waystone: any danger wins, then warnings, then wanted mods. */
export function verdictFor(templates: readonly string[], flags: Flags): Verdict {
  const count = (f: ModFlag) => templates.filter((t) => flagFor(t, flags) === f).length;
  const danger = count("danger");
  const warn = count("warn");
  const good = count("good");
  const kind = danger > 0 ? "danger" : warn > 0 ? "warn" : good > 0 ? "good" : "clear";
  return { kind, danger, warn, good };
}
