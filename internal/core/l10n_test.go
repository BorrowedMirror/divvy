package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pairs(p ...string) [][2]string {
	out := make([][2]string, 0, len(p)/2)
	for i := 0; i+1 < len(p); i += 2 {
		out = append(out, [2]string{p[i], p[i+1]})
	}
	return out
}

func german() Language {
	return Language{
		Name: "German",
		Code: "de",
		Keys: pairs(
			"Item Class", "Gegenstandsklasse",
			"Rarity", "Seltenheit",
			"Item Level", "Gegenstandsstufe",
			"Requires", "Erfordert",
			"Armour", "Rüstung",
		),
		Rarities: pairs("Rare", "Selten", "Unique", "Einzigartig", "Normal", "Normal"),
		Flags:    pairs("Corrupted", "Verderbt"),
		Words:    pairs("Level", "Stufe", "Dex", "Ges"),
		Patterns: pairs(
			"Superior {0}", "{0} (hochwertig)",
			"Prefix Modifier \"{0}\"", "Präfix-Modifikator \"{0}\"",
			" (Tier: {0})", " (Level: {0})",
		),
		Classes: pairs("Rings", "Ringe", "Helmets", "Helme"),
		Bases:   pairs("Sapphire Ring", "Saphirring", "Iron Hat", "Eisenhut"),
		Uniques: pairs("Headhunter", "Kopfjäger"),
		Stats: pairs(
			"{0:+d} to maximum Life", "{0:+d} zu maximalem Leben",
			"{0}% increased Armour", "{0}% erhöhte Rüstung",
			"{0}% reduced Armour", "{0}% verringerte Rüstung",
			"Adds {0} to {1} Fire Damage", "Fügt {0} bis {1} Feuerschaden hinzu",
			"{0}% faster start of Energy Shield Recharge", "Energieschild-Aufladung beginnt {0}% schneller",
			"{0:+d}% to Cold Resistance", "{0:+d}% zu Kältewiderstand",
		),
	}
}

func TestTranslatesARareItem(t *testing.T) {
	tr := NewTranslator([]Language{german()})
	text := "Gegenstandsklasse: Ringe\nSeltenheit: Selten\nGale Loop\nSaphirring\n--------\nErfordert: Stufe 40, 13 Ges\n--------\nGegenstandsstufe: 75\n--------\n+20% zu Kältewiderstand (implicit)\n--------\n+52 zu maximalem Leben\n10% verringerte Rüstung\nFügt 3 bis 7 Feuerschaden hinzu\nEnergieschild-Aufladung beginnt 12,5% schneller\nEtwas Unbekanntes\n--------\nVerderbt"
	out, ok := tr.Translate(text)
	require.True(t, ok)
	assert.Equal(t, "German", out.Language)
	assert.Equal(t,
		"Item Class: Rings\nRarity: Rare\nGale Loop\nSapphire Ring\n--------\nRequires: Level 40, 13 Dex\n--------\nItem Level: 75\n--------\n+20% to Cold Resistance (implicit)\n--------\n+52 to maximum Life\n10% reduced Armour\nAdds 3 to 7 Fire Damage\n12.5% faster start of Energy Shield Recharge\nEtwas Unbekanntes\n--------\nCorrupted",
		out.Text)
}

func TestNamesSuperiorMagicUniqueAndAdvancedHeaders(t *testing.T) {
	tr := NewTranslator([]Language{german()})
	normal, ok := tr.Translate("Gegenstandsklasse: Helme\nSeltenheit: Normal\nEisenhut (hochwertig)\n--------\nRüstung: 12")
	require.True(t, ok)
	assert.Equal(t, "Item Class: Helmets\nRarity: Normal\nSuperior Iron Hat\n--------\nArmour: 12", normal.Text)
	magic, ok := tr.Translate("Gegenstandsklasse: Ringe\nSeltenheit: Magisch\nFunkelnder Saphirring des Fuchses")
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(magic.Text, "\nSapphire Ring"), magic.Text)
	unique, ok := tr.Translate("Gegenstandsklasse: Ringe\nSeltenheit: Einzigartig\nKopfjäger\nSaphirring")
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(unique.Text, "Rarity: Unique\nHeadhunter\nSapphire Ring"), unique.Text)
	adv, ok := tr.Translate("Gegenstandsklasse: Ringe\nSeltenheit: Selten\nX\nSaphirring\n--------\n{ Präfix-Modifikator \"Gesund\" (Level: 7) — Leben }\n+42(40-49) zu maximalem Leben")
	require.True(t, ok)
	assert.True(t,
		strings.HasSuffix(adv.Text, "{ Prefix Modifier \"Gesund\" (Tier: 7) — Leben }\n+42(40-49) to maximum Life"),
		adv.Text)
}

func TestEnglishAndUnknownTextAreLeftAlone(t *testing.T) {
	tr := NewTranslator([]Language{german()})
	assert.False(t, translates(tr, "Item Class: Rings\nRarity: Rare\nX\nSapphire Ring"))
	assert.False(t, translates(tr, "Rarity: Gem\nCoil"))
	assert.False(t, translates(tr, "hello there"))
	assert.False(t, translates(tr, ""))
}

func TestReorderedPlaceholdersKeepTheirValues(t *testing.T) {
	lang := Language{
		Name:  "Japanese",
		Code:  "ja",
		Keys:  pairs("Item Class", "アイテムクラス", "Rarity", "レアリティ"),
		Stats: pairs("Adds {0} to {1} Fire Damage", "{1}から{0}の火ダメージを追加する"),
	}
	tr := NewTranslator([]Language{lang})
	out, ok := tr.Translate("アイテムクラス: 指輪\n--------\n7から3の火ダメージを追加する")
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(out.Text, "Adds 3 to 7 Fire Damage"), out.Text)
}

func translates(tr *Translator, text string) bool {
	_, ok := tr.Translate(text)
	return ok
}
