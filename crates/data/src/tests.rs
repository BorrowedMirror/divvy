use pretty_assertions::assert_eq;
use rkl_core::{
    catalog::{BaseDef, Catalog, StatAlias, StatDef, StaticDef, UniqueDef},
    item::ModKind,
};

use super::*;

fn stat(id: &str, kind: &str, text: &str, aliases: &[(&str, bool)]) -> StatDef {
    StatDef {
        id: id.into(),
        text: text.into(),
        kind: kind.into(),
        aliases: aliases.iter().map(|(t, n)| StatAlias { text: (*t).into(), negate: *n }).collect(),
    }
}

fn base(name: &str, category: &str) -> BaseDef {
    BaseDef { name: name.into(), category: category.into(), item_class: "Helmets".into() }
}

fn sample() -> Bundle {
    Bundle {
        stats: vec![
            stat(
                "explicit.life",
                "explicit",
                "# to maximum Life",
                &[("+# to maximum Life", false), ("-# to maximum Life", true)],
            ),
            stat("explicit.ms", "explicit", "#% increased Movement Speed", &[("#% reduced Movement Speed", true)]),
            stat("implicit.life", "implicit", "# to maximum Life", &[]),
            stat("explicit.dup_a", "explicit", "Dazes on Hit", &[]),
            stat("explicit.dup_b", "explicit", "Dazes on Hit", &[("Dazes on Hit", false)]),
            stat("desecrated.life", "desecrated", "# to maximum Life", &[]),
            stat("skill.raise", "skill", "Grants Skill: Level # Raise Shield", &[]),
            stat("rune.life", "rune", "Bonded: # to maximum Life", &[]),
            stat("sanctum.honour", "sanctum", "#% increased Honour restored", &[]),
            stat("sanctum.life", "sanctum", "# to maximum Life", &[]),
            stat("explicit.armour_local", "explicit", "#% increased Armour (Local)", &[("#% increased Armour", false)]),
            stat("explicit.armour", "explicit", "#% increased Armour", &[]),
        ],
        bases: vec![
            base("Hood", "armour.helmet"),
            base("Hunter Hood", "armour.helmet"),
            base("Runeforged Hunter Hood", "armour.helmet"),
            base("Gold Ring", "accessory.ring"),
        ],
        uniques: vec![
            UniqueDef { name: "Elevore".into(), base_type: "Hunter Hood".into() },
            UniqueDef { name: "Elevore".into(), base_type: "Runemastered Hunter Hood".into() },
        ],
        statics: vec![StaticDef {
            id: "divine".into(),
            text: "Divine Orb".into(),
            group: "Currency".into(),
            image: None,
        }],
    }
}

fn sources() -> AssetSources {
    AssetSources {
        stats: vec!["https://example.invalid/stats".into()],
        bases: vec!["https://example.invalid/items".into()],
        uniques: vec!["https://example.invalid/items".into()],
        statics: vec!["https://example.invalid/static".into()],
    }
}

fn write_sample(dir: &Path) -> Manifest {
    write_dir(dir, &sample(), &sources(), "2026-01-01T00:00:00Z".into(), Some("0.0.1".into())).unwrap()
}

fn data() -> GameData {
    let dir = tempfile::tempdir().unwrap();
    write_sample(dir.path());
    load_dir(dir.path()).unwrap()
}

// ---- asset encoding ----

#[test]
fn encode_decode_round_trip() {
    let stats = sample().stats;
    let (entry, bytes) = encode_asset("stats.json.zst", vec!["u".into()], &stats);
    assert_eq!(entry.records, stats.len());
    assert_eq!(entry.sha256.len(), 64);
    let back: Vec<StatDef> = decode_asset(&entry, &bytes).unwrap();
    assert_eq!(back, stats);
}

#[test]
fn hash_is_case_insensitive() {
    let (mut entry, bytes) = encode_asset("x", vec![], &[1, 2, 3]);
    entry.sha256 = entry.sha256.to_uppercase();
    let back: Vec<u32> = decode_asset(&entry, &bytes).unwrap();
    assert_eq!(back, [1, 2, 3]);
}

#[test]
fn sha256_known_vector() {
    assert_eq!(sha256_hex(b"abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
}

#[test]
fn hash_mismatch_rejected() {
    let (mut entry, bytes) = encode_asset("bases.json.zst", vec![], &sample().bases);
    entry.sha256 = "0".repeat(64);
    let err = decode_asset::<Vec<BaseDef>>(&entry, &bytes).unwrap_err();
    assert!(matches!(&err, DataError::Hash { file, .. } if file == "bases.json.zst"), "{err:?}");
    assert!(err.to_string().contains("hash mismatch"));
}

#[test]
fn corrupt_zstd_rejected() {
    let (entry, mut bytes) = encode_asset("stats.json.zst", vec![], &sample().stats);
    bytes.truncate(bytes.len() / 2);
    let err = decode_asset::<Vec<StatDef>>(&entry, &bytes).unwrap_err();
    assert!(matches!(err, DataError::Decompress(..)), "{err:?}");
    let err = decode_asset::<Vec<StatDef>>(&entry, b"not zstd at all").unwrap_err();
    assert!(err.to_string().contains("failed to decompress"), "{err}");
}

#[test]
fn wrong_shape_json_rejected() {
    let (entry, bytes) = encode_asset("statics.json.zst", vec![], &[1, 2, 3]);
    let err = decode_asset::<Vec<StaticDef>>(&entry, &bytes).unwrap_err();
    assert!(matches!(err, DataError::Json(ref f, _) if f == "statics.json.zst"), "{err:?}");
    assert!(err.to_string().contains("not valid JSON"));
}

// ---- manifest / directories ----

#[test]
fn schema_mismatch_rejected() {
    let dir = tempfile::tempdir().unwrap();
    let mut manifest = write_sample(dir.path());
    manifest.schema_version = SCHEMA_VERSION + 1;
    std::fs::write(dir.path().join(MANIFEST_FILE), serde_json::to_vec(&manifest).unwrap()).unwrap();
    let err = load_dir(dir.path()).unwrap_err();
    assert!(matches!(err, DataError::Schema(v) if v == SCHEMA_VERSION + 1), "{err:?}");
    assert!(err.to_string().contains("unsupported schema version"));
}

#[test]
fn invalid_manifest_rejected() {
    let err = parse_manifest(b"{}").unwrap_err();
    assert!(matches!(err, DataError::Json(ref f, _) if f == MANIFEST_FILE), "{err:?}");
}

#[test]
fn missing_manifest_rejected() {
    let dir = tempfile::tempdir().unwrap();
    let err = load_dir(dir.path()).unwrap_err();
    assert!(matches!(err, DataError::Io(ref f, _) if f == MANIFEST_FILE), "{err:?}");
    assert!(err.to_string().contains("could not be read"));
}

#[test]
fn missing_asset_file_rejected() {
    let dir = tempfile::tempdir().unwrap();
    write_sample(dir.path());
    std::fs::remove_file(dir.path().join("uniques.json.zst")).unwrap();
    let err = load_dir(dir.path()).unwrap_err();
    assert!(matches!(err, DataError::Io(ref f, _) if f == "uniques.json.zst"), "{err:?}");
}

#[test]
fn tampered_asset_file_rejected() {
    let dir = tempfile::tempdir().unwrap();
    write_sample(dir.path());
    let (_, other) = encode_asset("statics.json.zst", vec![], &Vec::<StaticDef>::new());
    std::fs::write(dir.path().join("statics.json.zst"), other).unwrap();
    assert!(matches!(load_dir(dir.path()), Err(DataError::Hash { .. })));
}

#[test]
fn write_dir_reports_io_errors() {
    let dir = tempfile::tempdir().unwrap();
    let file = dir.path().join("not-a-dir");
    std::fs::write(&file, b"x").unwrap();
    let err = write_dir(&file, &sample(), &sources(), String::new(), None).unwrap_err();
    assert!(matches!(err, DataError::Io(..)), "{err:?}");
    // Directory exists but an asset path is occupied by a directory.
    let dir2 = tempfile::tempdir().unwrap();
    std::fs::create_dir(dir2.path().join("stats.json.zst")).unwrap();
    assert!(matches!(write_dir(dir2.path(), &sample(), &sources(), String::new(), None), Err(DataError::Io(..))));
    let dir3 = tempfile::tempdir().unwrap();
    std::fs::create_dir(dir3.path().join(MANIFEST_FILE)).unwrap();
    assert!(matches!(write_dir(dir3.path(), &sample(), &sources(), String::new(), None), Err(DataError::Io(..))));
}

#[test]
fn load_dir_round_trip() {
    let dir = tempfile::tempdir().unwrap();
    let manifest = write_sample(dir.path());
    let data = load_dir(dir.path()).unwrap();
    assert_eq!(data.manifest(), &manifest);
    assert_eq!(data.manifest, manifest);
    assert_eq!(data.bundle(), &sample());
    assert_eq!(manifest.stats.records, sample().stats.len());
    assert_eq!(manifest.game_version.as_deref(), Some("0.0.1"));
}

#[test]
fn embedded_missing_file_rejected() {
    let err = decode_embedded(embedded::MANIFEST, &embedded::FILES[..2]).unwrap_err();
    assert!(matches!(err, DataError::Io(ref f, _) if f == "uniques.json.zst"), "{err:?}");
}

// ---- catalog ----

fn ids(hits: &[(&StatDef, bool)]) -> Vec<(String, bool)> {
    hits.iter().map(|(s, n)| (s.id.clone(), *n)).collect()
}

#[test]
fn stats_by_template_direct_alias_and_kind() {
    let d = data();
    assert_eq!(ids(&d.stats_by_template("# to maximum Life", ModKind::Explicit)), [("explicit.life".into(), false)]);
    assert_eq!(ids(&d.stats_by_template("+# to maximum Life", ModKind::Explicit)), [("explicit.life".into(), false)]);
    assert_eq!(ids(&d.stats_by_template("-# to maximum Life", ModKind::Explicit)), [("explicit.life".into(), true)]);
    assert_eq!(
        ids(&d.stats_by_template("#% reduced Movement Speed", ModKind::Explicit)),
        [("explicit.ms".into(), true)]
    );
    assert_eq!(ids(&d.stats_by_template("# to maximum Life", ModKind::Implicit)), [("implicit.life".into(), false)]);
    assert_eq!(
        ids(&d.stats_by_template("# to maximum Life", ModKind::Desecrated)),
        [("desecrated.life".into(), false)]
    );
    assert_eq!(ids(&d.stats_by_template("# to maximum Life", ModKind::Sanctified)), [("explicit.life".into(), false)]);
    assert_eq!(ids(&d.stats_by_template("Bonded: # to maximum Life", ModKind::Rune)), [("rune.life".into(), false)]);
    assert_eq!(
        ids(&d.stats_by_template("Grants Skill: Level # Raise Shield", ModKind::Skill)),
        [("skill.raise".into(), false)]
    );
    assert!(d.stats_by_template("# to maximum Life", ModKind::Enchant).is_empty());
    assert!(d.stats_by_template("# to maximum Life", ModKind::Crafted).is_empty());
    assert!(d.stats_by_template("# to maximum Life", ModKind::Fractured).is_empty());
    assert!(d.stats_by_template("nope", ModKind::Explicit).is_empty());
}

#[test]
fn stats_by_template_sanctum_fallback_and_direct_first() {
    let d = data();
    assert_eq!(
        ids(&d.stats_by_template("#% increased Honour restored", ModKind::Explicit)),
        [("sanctum.honour".into(), false)]
    );
    assert_eq!(
        ids(&d.stats_by_template("#% increased Honour restored", ModKind::Implicit)),
        [("sanctum.honour".into(), false)]
    );
    assert!(d.stats_by_template("#% increased Honour restored", ModKind::Rune).is_empty());
    // Fallback only when the primary kind has no hit.
    assert_eq!(ids(&d.stats_by_template("# to maximum Life", ModKind::Explicit)), [("explicit.life".into(), false)]);
    // The real template is listed before stats that reach it via an alias.
    assert_eq!(
        ids(&d.stats_by_template("#% increased Armour", ModKind::Explicit)),
        [("explicit.armour".into(), false), ("explicit.armour_local".into(), false)]
    );
}

#[test]
fn stats_by_template_ambiguous_and_deduped() {
    let d = data();
    // Two stats share a template; a self-alias must not duplicate the hit.
    assert_eq!(
        ids(&d.stats_by_template("Dazes on Hit", ModKind::Explicit)),
        [("explicit.dup_a".into(), false), ("explicit.dup_b".into(), false)]
    );
}

#[test]
fn base_lookups() {
    let d = data();
    assert_eq!(d.base("Hunter Hood").unwrap().name, "Hunter Hood");
    assert!(d.base("hunter hood").is_none());
    assert_eq!(d.base_in_magic_name("Sturdy Hunter Hood of the Fox").unwrap().name, "Hunter Hood");
    assert_eq!(d.base_in_magic_name("Runeforged Hunter Hood of Ire").unwrap().name, "Runeforged Hunter Hood");
    assert_eq!(d.base_in_magic_name("Hunter Hood").unwrap().name, "Hunter Hood");
    assert_eq!(d.base_in_magic_name("Shiny Hood").unwrap().name, "Hood");
    assert_eq!(d.base_in_magic_name("Glinting Gold Ring of the Lynx").unwrap().category, "accessory.ring");
    // Whole words only: `Hoodie` must not match `Hood`.
    assert!(d.base_in_magic_name("Cosy Hoodie").is_none());
    assert!(d.base_in_magic_name("").is_none());
    // Irregular whitespace is normalised before matching word runs.
    assert_eq!(d.base_in_magic_name("Sturdy  Hunter\tHood of the Fox").unwrap().name, "Hunter Hood");
}

#[test]
fn unique_and_static_lookups() {
    let d = data();
    assert_eq!(d.unique("Elevore").unwrap().base_type, "Hunter Hood");
    assert!(d.unique("Nope").is_none());
    assert_eq!(d.static_item("Divine Orb").unwrap().id, "divine");
    assert!(d.static_item("Mirror").is_none());
}

#[test]
fn clone_shares_indexes() {
    let d = data();
    let c = d.clone();
    assert!(std::ptr::eq(d.bundle(), c.bundle()));
}

// ---- bundled assets ----

#[test]
fn bundled_assets_load() {
    let d = bundled().unwrap();
    assert!(d.bundle().stats.len() > 5000, "{} stats", d.bundle().stats.len());
    assert!(d.bundle().bases.len() > 1000);
    assert!(d.bundle().uniques.len() > 300);
    assert!(d.bundle().statics.len() > 300);
    assert_eq!(d.manifest.schema_version, SCHEMA_VERSION);
    assert_eq!(d.static_item("Divine Orb").unwrap().id, "divine");
    let hood = d.base("Hunter Hood").unwrap();
    assert_eq!((hood.category.as_str(), hood.item_class.as_str()), ("armour.helmet", "Helmets"));
    assert_eq!(d.base_in_magic_name("Sturdy Hunter Hood of the Fox").unwrap().name, "Hunter Hood");
    assert!(d.unique("Astramentis").is_some());
    let life = d.stats_by_template("+# to maximum Life", ModKind::Explicit);
    assert!(life.iter().any(|(s, negate)| s.id == "explicit.stat_3299347043" && !negate), "{life:?}");
    let has =
        |template: &str, kind: ModKind, id: &str| d.stats_by_template(template, kind).iter().any(|(s, _)| s.id == id);
    assert!(has("#% increased Armour", ModKind::Explicit, "explicit.stat_1062208444"));
    assert!(has("#% increased Duration", ModKind::Explicit, "explicit.stat_2541588185"));
    assert!(has("#% increased Honour restored", ModKind::Explicit, "sanctum.stat_1583320325"));
    assert!(has("#% increased Quantity of Items found in your Maps", ModKind::Explicit, "explicit.stat_2390685262"));
    assert!(has("#% increased Experience gain in your Maps", ModKind::Explicit, "explicit.stat_57434274"));
    // Records are sorted deterministically.
    assert!(d.bundle().bases.windows(2).all(|w| w[0].name < w[1].name));
}

#[test]
fn bundled_is_cached() {
    let a = bundled_static();
    let b = bundled_static();
    assert!(std::ptr::eq(a, b));
    let c = bundled().unwrap();
    assert!(std::ptr::eq(a.bundle(), c.bundle()));
}

#[test]
fn bundled_manifest_snapshot() {
    insta::assert_json_snapshot!(bundled_static().manifest, {
        ".generated_at" => "[generated_at]",
        ".*.sha256" => "[sha256]",
    });
}

#[test]
fn uniques_by_base_lists_every_unique_for_a_base() {
    use rkl_core::catalog::Catalog;
    let data = crate::bundled().unwrap();
    let names: Vec<&str> = data.uniques_by_base("Gold Ring").iter().map(|u| u.name.as_str()).collect();
    assert!(names.len() >= 2, "{names:?}");
    assert!(names.windows(2).all(|w| w[0] < w[1]), "sorted, no duplicates: {names:?}");
    assert!(data.uniques_by_base("Not A Base").is_empty());
}
