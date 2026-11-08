package app

import (
	"net/url"
	"strings"
)

// LookupSite is a site an item can be looked up on from the panel. Sent to the frontend.
type LookupSite string

// LookupSite values.
const (
	LookupPoe2db       LookupSite = "poe2db"
	LookupWiki         LookupSite = "wiki"
	LookupCraftOfExile LookupSite = "craft_of_exile"
)

// lookupSites lists every [LookupSite] in declaration order.
var lookupSites = []LookupSite{LookupPoe2db, LookupWiki, LookupCraftOfExile}

// UnmarshalText accepts only the [LookupSite] values.
func (s *LookupSite) UnmarshalText(b []byte) error {
	return parseEnum(s, b, lookupSites...)
}

// lookupURL is the page for name (a unique's name or a base type) on site. The hosts are fixed
// here, so the web view can only open these three sites, and name is always one escaped path
// segment. Craft of Exile imports itemText (the game's copied item text, advanced Ctrl+Alt+C
// for mods) from its `eimport` query parameter, which its page submits to its Item Importer on
// load. ok is false when name is blank (except for Craft of Exile, which ignores it).
func lookupURL(site LookupSite, name, itemText string) (u string, ok bool) {
	if site == LookupCraftOfExile {
		u = "https://beta.craftofexile.com/?game=poe2"
		text := strings.ReplaceAll(itemText, "\r\n", "\n")
		if strings.TrimSpace(text) != "" {
			u += "&eimport=" + url.QueryEscape(text)
		}
		return u, true
	}
	page := strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
	if page == "" {
		return "", false
	}
	base := "https://www.poe2wiki.net/wiki/"
	if site == LookupPoe2db {
		base = "https://poe2db.tw/us/"
	}
	return base + url.PathEscape(page), true
}
