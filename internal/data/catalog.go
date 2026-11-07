package data

import (
	"cmp"
	"slices"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// statKinds returns the trade stat kinds ([core.StatDef.Kind], the trade2 stats group id)
// searched for a modifier kind.
//
// trade2 has no dedicated group for sanctified modifiers; they are searched as explicits.
func statKinds(kind core.ModKind) []string {
	switch kind {
	case core.ModImplicit:
		return []string{"implicit"}
	case core.ModExplicit, core.ModSanctified:
		return []string{"explicit"}
	case core.ModEnchant:
		return []string{"enchant"}
	case core.ModRune:
		return []string{"rune"}
	case core.ModCrafted:
		return []string{"crafted"}
	case core.ModFractured:
		return []string{"fractured"}
	case core.ModDesecrated:
		return []string{"desecrated"}
	case core.ModSkill:
		return []string{"skill"}
	}
	return nil
}

// fallbackStatKinds returns the kinds searched only when [statKinds] finds nothing: relic
// (and sanctum) modifiers print as plain explicits/implicits but trade2 files them under
// `sanctum`.
func fallbackStatKinds(kind core.ModKind) []string {
	switch kind {
	case core.ModImplicit, core.ModExplicit, core.ModSanctified:
		return []string{"sanctum"}
	}
	return nil
}

// statHit is one stat reachable from a template: its index in the bundle and whether the
// template is a negated alias.
type statHit struct {
	index  int
	negate bool
}

// statIndex maps kind -> template -> hits.
type statIndex map[string]map[string][]statHit

type indexed struct {
	bundle     Bundle
	translator *core.Translator
	stats      statIndex
	bases      map[string]int
	uniques    map[string]int
	statics    map[string]int
}

func newIndexed(bundle Bundle) *indexed {
	// Pre-size each kind's map (upper bound: templates + aliases) to avoid rehashing.
	sizes := map[string]int{}
	for i := range bundle.Stats {
		sizes[bundle.Stats[i].Kind] += 1 + len(bundle.Stats[i].Aliases)
	}
	stats := make(statIndex, len(sizes))
	add := func(kind, template string, i int, negate bool) {
		byTemplate, ok := stats[kind]
		if !ok {
			byTemplate = make(map[string][]statHit, sizes[kind])
			stats[kind] = byTemplate
		}
		hits := byTemplate[template]
		if slices.ContainsFunc(hits, func(h statHit) bool { return h.index == i }) {
			return
		}
		byTemplate[template] = append(hits, statHit{index: i, negate: negate})
	}
	// Real templates first, then aliases, so direct hits always come first.
	for i := range bundle.Stats {
		add(bundle.Stats[i].Kind, bundle.Stats[i].Text, i, false)
	}
	for i := range bundle.Stats {
		for _, alias := range bundle.Stats[i].Aliases {
			add(bundle.Stats[i].Kind, alias.Text, i, alias.Negate)
		}
	}
	return &indexed{
		stats:      stats,
		bases:      firstIndex(bundle.Bases, func(b *core.BaseDef) string { return b.Name }),
		uniques:    firstIndex(bundle.Uniques, func(u *core.UniqueDef) string { return u.Name }),
		statics:    firstIndex(bundle.Statics, func(s *core.StaticDef) string { return s.Text }),
		translator: core.NewTranslator(bundle.Languages),
		bundle:     bundle,
	}
}

// firstIndex maps each key to the index of its first occurrence.
func firstIndex[T any](items []T, key func(*T) string) map[string]int {
	m := make(map[string]int, len(items))
	for i := range items {
		k := key(&items[i])
		if _, ok := m[k]; !ok {
			m[k] = i
		}
	}
	return m
}

// GameData is indexed, query-ready game data implementing [core.Catalog]. It is
// immutable and safe for concurrent use.
//
// Copying a GameData value is cheap: the bundle and its indexes are shared.
type GameData struct {
	Manifest Manifest
	inner    *indexed
}

var _ core.Catalog = (*GameData)(nil)

// FromBundle indexes a decoded bundle.
func FromBundle(m Manifest, bundle Bundle) *GameData {
	return &GameData{Manifest: m, inner: newIndexed(bundle)}
}

// Bundle returns the decoded records (shared; do not modify).
func (d *GameData) Bundle() *Bundle { return &d.inner.bundle }

// Translate returns item text from a non-English client in English; nil for English text.
func (d *GameData) Translate(text string) *core.Translated { return d.inner.translator.Translate(text) }

// StatsByTemplate implements [core.Catalog].
func (d *GameData) StatsByTemplate(template string, kind core.ModKind) []core.StatMatch {
	lookup := func(kinds []string) []core.StatMatch {
		var out []core.StatMatch
		for _, k := range kinds {
			for _, h := range d.inner.stats[k][template] {
				out = append(out, core.StatMatch{Stat: &d.inner.bundle.Stats[h.index], Negate: h.negate})
			}
		}
		return out
	}
	if hits := lookup(statKinds(kind)); len(hits) > 0 {
		return hits
	}
	return lookup(fallbackStatKinds(kind))
}

// Base implements [core.Catalog].
func (d *GameData) Base(name string) *core.BaseDef {
	if i, ok := d.inner.bases[name]; ok {
		return &d.inner.bundle.Bases[i]
	}
	return nil
}

// BaseInMagicName implements [core.Catalog]: the longest base name that appears as a
// whole-word run inside fullName (e.g. `Hunter Hood` in `Sturdy Hunter Hood of the Fox`).
func (d *GameData) BaseInMagicName(fullName string) *core.BaseDef {
	// Normalise whitespace once so every word run is a plain substring of joined (no
	// per-candidate allocation); bounds[i] is the byte range of word i.
	words := strings.Fields(fullName)
	joined := strings.Join(words, " ")
	bounds := make([][2]int, len(words))
	pos := 0
	for i, w := range words {
		bounds[i] = [2]int{pos, pos + len(w)}
		pos += len(w) + 1
	}
	var best *core.BaseDef
	for start := range bounds {
		from := bounds[start][0]
		for _, b := range bounds[start:] {
			candidate := joined[from:b[1]]
			if best != nil && len(best.Name) >= len(candidate) {
				continue
			}
			if base := d.Base(candidate); base != nil {
				best = base
			}
		}
	}
	return best
}

// Unique implements [core.Catalog].
func (d *GameData) Unique(name string) *core.UniqueDef {
	if i, ok := d.inner.uniques[name]; ok {
		return &d.inner.bundle.Uniques[i]
	}
	return nil
}

// StaticItem implements [core.Catalog].
func (d *GameData) StaticItem(text string) *core.StaticDef {
	if i, ok := d.inner.statics[text]; ok {
		return &d.inner.bundle.Statics[i]
	}
	return nil
}

// UniquesByBase implements [core.Catalog]: every unique on baseType, sorted by name,
// one per name.
func (d *GameData) UniquesByBase(baseType string) []*core.UniqueDef {
	var out []*core.UniqueDef
	for i := range d.inner.bundle.Uniques {
		if d.inner.bundle.Uniques[i].BaseType == baseType {
			out = append(out, &d.inner.bundle.Uniques[i])
		}
	}
	slices.SortStableFunc(out, func(a, b *core.UniqueDef) int { return cmp.Compare(a.Name, b.Name) })
	return slices.CompactFunc(out, func(a, b *core.UniqueDef) bool { return a.Name == b.Name })
}
