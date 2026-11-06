import { Toast } from "@base-ui/react/toast";
import { useTranslation } from "react-i18next";
import type { ParsedItem_Serialize } from "../../bindings";
import { commands } from "../../bindings";
import { useCommand } from "../../hooks/useCommand";

type Site = "poe2db" | "wiki" | "craft_of_exile";

/** Name to look up: a unique's own name, otherwise the base type (a rare's name is random). */
export function lookupName(item: ParsedItem_Serialize): string {
  return item.rarity === "unique" && item.name ? item.name : item.base_type;
}

/** Links that open the item on poe2db, the wiki and (equipment and jewels) Craft of Exile. */
export function ItemLinks({ item }: { item: ParsedItem_Serialize }) {
  const { t } = useTranslation();
  const command = useCommand();
  const toast = Toast.useToastManager();
  const open = async (site: Site) => {
    const res = await command(commands.openLookup(site, lookupName(item)));
    if (site === "craft_of_exile" && res.status === "ok" && res.data) {
      toast.add({ title: t("links.craftCopied"), description: t("links.craftCopiedHelp"), timeout: 6000 });
    }
  };
  const craftable = item.category === "equipment" || item.category === "jewel";
  const sites: Site[] = craftable ? ["poe2db", "wiki", "craft_of_exile"] : ["poe2db", "wiki"];
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
