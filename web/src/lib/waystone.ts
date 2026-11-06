export type PhraseLists = { avoid?: readonly string[]; want?: readonly string[] };

export type ModVerdict = "avoid" | "want" | null;

/** Whether a waystone mod matches one of the user's phrases (case-insensitive, anywhere in
 * the mod). "Avoid" wins when a mod matches both lists. */
export function classifyMod(text: string, lists: PhraseLists): ModVerdict {
  const t = text.toLowerCase();
  const hit = (phrases: readonly string[] = []) => phrases.some((p) => p.trim() !== "" && t.includes(p.trim().toLowerCase()));
  if (hit(lists.avoid)) return "avoid";
  if (hit(lists.want)) return "want";
  return null;
}

/** One phrase per line in the settings text box, blank lines dropped. */
export function parsePhrases(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "");
}
