import { useTranslation } from "react-i18next";
import type { ParsedItem_Serialize } from "@src/bindings";
import { commands } from "@src/bindings";
import { useSettingsQuery } from "@hooks/queries";
import { useCommand } from "@hooks/useCommand";

type Site = "poe2db" | "wiki" | "craft_of_exile";

/** Name to look up: a unique's own name, otherwise the base type (a rare's name is random). */
export function lookupName(item: ParsedItem_Serialize): string {
  return item.rarity === "unique" && item.name ? item.name : item.base_type;
}

/** Links that open the item on poe2db, the wiki and (equipment and jewels) Craft of Exile. */
export function ItemLinks({ item }: { item: ParsedItem_Serialize }) {
  const { t } = useTranslation();
  const command = useCommand();
  // Craft of Exile receives the copied item text in its link and imports every affix.
  const open = (site: Site) => command(commands.openLookup(site, lookupName(item)));
  const enabled = useSettingsQuery().data?.overlay.links ?? { poe2db: true, wiki: true, craft_of_exile: true };
  const craftable = item.category === "equipment" || item.category === "jewel";
  const sites = (["poe2db", "wiki", "craft_of_exile"] as const).filter(
    (s) => enabled[s] && (s !== "craft_of_exile" || craftable),
  );
  if (sites.length === 0) return null;
  return (
    <nav aria-label={t("links.label")} className="flex flex-wrap gap-x-3 gap-y-1 text-xs" data-testid="item-links">
      {sites.map((site) => (
        <button
          key={site}
          type="button"
          className="text-gold underline-offset-2 hover:underline outline-none focus-visible:ring-2 focus-visible:ring-gold"
          onClick={() => void open(site)}
        >
          {t(`links.${site}`)}
        </button>
      ))}
    </nav>
  );
}
