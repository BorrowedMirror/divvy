//! Behavioural tests for rkl-config: paths, migration, validation, load/save.
use std::path::{Path, PathBuf};

use pretty_assertions::assert_eq;
use proptest::prelude::*;
use rkl_config::{
    CONFIG_DIR_ENV, ConfigError, OverlaySide, Paths, SETTINGS_VERSION, Settings, Theme, load, migrate, save, validate,
};
use rkl_core::pricing::PriceSourceKind;
use rkl_platform::{BackendKind, Chord, KeyModifier};

fn fixture(name: &str) -> String {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures").join(name);
    std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()))
}

fn table(text: &str) -> toml::Table {
    toml::from_str(text).expect("valid toml")
}

fn write_settings(paths: &Paths, text: &str) {
    std::fs::create_dir_all(&paths.config_dir).unwrap();
    std::fs::write(&paths.settings_file, text).unwrap();
}

fn assert_invalid(result: Result<(), ConfigError>, expected_field: &str) {
    match result {
        Err(ConfigError::Invalid { field, .. }) => assert_eq!(field, expected_field),
        other => panic!("expected Invalid({expected_field}), got {other:?}"),
    }
}

// ---------------------------------------------------------------- paths

#[test]
fn resolve_honours_env_override() {
    let paths = Paths::resolve_with(|k| (k == CONFIG_DIR_ENV).then(|| "/portable/rkl".to_owned())).unwrap();
    assert_eq!(paths, Paths::under(Path::new("/portable/rkl")));
    assert_eq!(paths.settings_file, PathBuf::from("/portable/rkl/config/settings.toml"));
}

#[test]
fn resolve_ignores_blank_override_and_uses_project_dirs() {
    let paths = Paths::resolve_with(|_| Some("  ".to_owned())).unwrap();
    let dirs =
        directories::ProjectDirs::from(rkl_config::PROJECT_QUALIFIER, rkl_config::PROJECT_ORG, rkl_config::PROJECT_APP)
            .unwrap();
    assert_eq!(paths.config_dir, dirs.config_dir());
    assert_eq!(paths.cache_dir, dirs.cache_dir());
    assert_eq!(paths.settings_file, dirs.config_dir().join("settings.toml"));
    assert!(paths.log_dir.ends_with("logs"));
    assert_eq!(Paths::resolve_with(|_| None).unwrap(), paths);
}

#[test]
fn resolve_uses_process_env() {
    // Whatever the environment holds, resolve() must agree with resolve_with(std::env::var).
    let expected = Paths::resolve_with(|k| std::env::var(k).ok()).unwrap();
    assert_eq!(Paths::resolve().unwrap(), expected);
}

// ---------------------------------------------------------------- serde

#[test]
fn default_settings_toml_snapshot() {
    insta::assert_snapshot!(toml::to_string_pretty(&Settings::default()).unwrap());
}

#[test]
fn default_settings_are_valid() {
    validate(&Settings::default()).unwrap();
}

#[test]
fn settings_roundtrip_through_toml() {
    let mut s = Settings::default();
    s.league.auto_detect = false;
    s.league.league = Some("Hardcore \"Quoted\"".into());
    s.hotkeys.open_settings = Chord::new(&[KeyModifier::Meta], "F5");
    s.trade.source = PriceSourceKind::Poe2Scout;
    s.trade.contact = Some("exile@example.com".into());
    s.trade.poesessid = Some("abc123".into());
    s.overlay.side = OverlaySide::Left;
    s.ui.theme = Theme::Light;
    s.platform.backend_override = Some(BackendKind::Wayland);
    let text = toml::to_string(&s).unwrap();
    let back: Settings = toml::from_str(&text).unwrap();
    assert_eq!(back, s);
}

#[test]
fn onboarding_done_defaults_to_false_and_roundtrips() {
    assert!(!Settings::default().ui.onboarding_done);
    let old: Settings = toml::from_str("version = 1\n[ui]\nlanguage = \"de\"\nstart_minimised = false\n").unwrap();
    assert!(!old.ui.onboarding_done);
    assert_eq!(old.ui.language, "de");
    let mut s = Settings::default();
    s.ui.onboarding_done = true;
    let back: Settings = toml::from_str(&toml::to_string(&s).unwrap()).unwrap();
    assert!(back.ui.onboarding_done);
}

#[test]
fn partial_document_fills_defaults() {
    let s: Settings = toml::from_str("version = 1\n[overlay]\nopacity = 0.5\n").unwrap();
    assert!((s.overlay.opacity - 0.5).abs() < f64::EPSILON);
    assert_eq!(s.overlay.auto_hide_ms, Settings::default().overlay.auto_hide_ms);
    assert_eq!(s.trade, Settings::default().trade);
}

#[test]
fn check_updates_defaults_on_and_survives_older_files() {
    assert!(Settings::default().ui.check_updates);
    let s: Settings = toml::from_str("version = 1\n[ui]\nlanguage = \"de\"\n").unwrap();
    assert!(s.ui.check_updates, "files written before the field existed keep updates on");
    let off: Settings = toml::from_str("version = 1\n[ui]\ncheck_updates = false\n").unwrap();
    assert!(!off.ui.check_updates);
    assert!(validate(&off).is_ok());
}

// ---------------------------------------------------------------- migrate

#[test]
fn migrate_v0_full_fixture() {
    let doc = migrate(table(&fixture("v0_full.toml"))).unwrap();
    let expected = table(
        r#"
        version = 1
        [league]
        auto_detect = false
        league = "Rise of the Abyssal"
        [hotkeys.price_check]
        modifiers = ["ctrl", "shift"]
        key = "KeyD"
        [trade]
        online_only = false
        [ui]
        theme = "dark"
        "#,
    );
    assert_eq!(doc, expected);
    let s: Settings = doc.try_into().unwrap();
    validate(&s).unwrap();
    assert_eq!(s.hotkeys.price_check, Chord::new(&[KeyModifier::Ctrl, KeyModifier::Shift], "KeyD"));
    assert_eq!(s.ui.theme, Theme::Dark);
}

#[test]
fn migrate_v0_empty_league_means_auto_detect() {
    let doc = migrate(table(&fixture("v0_auto_league.toml"))).unwrap();
    let s: Settings = doc.try_into().unwrap();
    assert!(s.league.auto_detect);
    assert_eq!(s.league.league, None);
    assert_eq!(s.hotkeys.price_check, Chord::new(&[KeyModifier::Alt], "f5"));
    assert!(s.trade.online_only, "absent online_only keeps the default");
}

#[test]
fn migrate_v0_already_sectioned() {
    let doc = migrate(table(&fixture("v0_sectioned.toml"))).unwrap();
    assert_eq!(doc["version"].as_integer(), Some(1));
    let s: Settings = doc.try_into().unwrap();
    assert!(!s.league.auto_detect);
    assert_eq!(s.league.league.as_deref(), Some("Standard"));
    assert!((s.trade.widen_pct - 25.0).abs() < f64::EPSILON);
}

#[test]
fn migrate_empty_document_gets_current_version() {
    let doc = migrate(toml::Table::new()).unwrap();
    assert_eq!(doc["version"].as_integer(), Some(i64::from(SETTINGS_VERSION)));
}

#[test]
fn migrate_current_version_is_identity() {
    let doc = table("version = 1\n[ui]\nlanguage = \"de\"\n");
    assert_eq!(migrate(doc.clone()).unwrap(), doc);
}

#[test]
fn migrate_rejects_newer_version() {
    let err = migrate(table("version = 2")).unwrap_err();
    assert!(matches!(err, ConfigError::TooNew { found: 2 }), "{err:?}");
    assert!(err.to_string().contains("newer"));
}

#[test]
fn migrate_rejects_bad_version_values() {
    for text in ["version = -1", "version = 4294967296", "version = \"1\""] {
        let err = migrate(table(text)).unwrap_err();
        assert!(matches!(&err, ConfigError::Invalid { field, .. } if field == "version"), "{text}: {err:?}");
    }
}

#[test]
fn migrate_v0_rejects_malformed_legacy_keys() {
    let cases = [
        ("league = 3", "league"),
        ("hotkey = 3", "hotkey"),
        ("hotkey = \"Ctrl+\"", "hotkey"),
        ("online_only = \"yes\"", "online_only"),
        ("online_only = true\ntrade = 1", "trade"),
    ];
    for (text, field) in cases {
        let err = migrate(table(text)).unwrap_err();
        assert!(matches!(&err, ConfigError::Invalid { field: f, .. } if f == field), "{text}: {err:?}");
    }
}

// ---------------------------------------------------------------- validate

#[test]
fn validate_ranges() {
    let check = |f: &dyn Fn(&mut Settings), field: &str| {
        let mut s = Settings::default();
        f(&mut s);
        assert_invalid(validate(&s), field);
    };
    check(&|s| s.trade.widen_pct = -0.1, "trade.widen_pct");
    check(&|s| s.trade.widen_pct = 100.5, "trade.widen_pct");
    check(&|s| s.trade.widen_pct = f64::NAN, "trade.widen_pct");
    check(&|s| s.overlay.opacity = 0.05, "overlay.opacity");
    check(&|s| s.overlay.opacity = 1.01, "overlay.opacity");
    check(&|s| s.overlay.auto_hide_ms = 10_001, "overlay.auto_hide_ms");

    let mut s = Settings::default();
    s.trade.widen_pct = 100.0;
    s.overlay.opacity = 0.1;
    s.overlay.auto_hide_ms = 10_000;
    validate(&s).unwrap();
}

#[test]
fn validate_league_required_without_auto_detect() {
    let mut s = Settings::default();
    s.league.auto_detect = false;
    assert_invalid(validate(&s), "league.league");
    s.league.league = Some("   ".into());
    assert_invalid(validate(&s), "league.league");
    s.league.league = Some("Standard".into());
    validate(&s).unwrap();
}

#[test]
fn validate_hotkeys_distinct_ignoring_modifier_order() {
    let mut s = Settings::default();
    s.hotkeys.open_settings = Chord::new(&[KeyModifier::Alt, KeyModifier::Ctrl], "KeyD");
    let err = validate(&s).unwrap_err();
    assert!(
        matches!(&err, ConfigError::Invalid { field, reason } if field == "hotkeys.open_settings" && reason.contains("price_check_pinned")),
        "{err:?}"
    );

    let mut s = Settings::default();
    s.hotkeys.price_check_pinned = s.hotkeys.price_check.clone();
    assert_invalid(validate(&s), "hotkeys.price_check_pinned");
}

#[test]
fn validate_chord_needs_non_modifier_key() {
    for key in ["", "  ", "ShiftLeft", "control", "MetaRight"] {
        let mut s = Settings::default();
        s.hotkeys.price_check = Chord::new(&[KeyModifier::Ctrl], key);
        assert_invalid(validate(&s), "hotkeys.price_check");
    }
}

#[test]
fn validate_contact_email() {
    for bad in ["", "exile", "@example.com", "exile@", "exile@localhost", "a@b@c.com", "ex ile@a.com", "e@a..com"] {
        let mut s = Settings::default();
        s.trade.contact = Some(bad.into());
        assert_invalid(validate(&s), "trade.contact");
    }
    let mut s = Settings::default();
    s.trade.contact = Some("exile.42+poe@mail.example.org".into());
    validate(&s).unwrap();
}

#[test]
fn validate_rejects_future_version() {
    let s = Settings { version: SETTINGS_VERSION + 1, ..Settings::default() };
    assert!(matches!(validate(&s), Err(ConfigError::TooNew { .. })));
}

// ---------------------------------------------------------------- load / save

#[test]
fn load_missing_file_yields_defaults() {
    let dir = tempfile::tempdir().unwrap();
    assert_eq!(load(&Paths::under(dir.path())).unwrap(), Settings::default());
}

#[test]
fn save_then_load_roundtrips_and_creates_dirs() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(&dir.path().join("deep/nested"));
    let mut s = Settings::default();
    s.ui.language = "de".into();
    s.trade.contact = Some("me@example.com".into());
    save(&paths, &s).unwrap();
    assert_eq!(load(&paths).unwrap(), s);

    // Overwrite in place; no temp files left behind.
    s.overlay.opacity = 0.5;
    save(&paths, &s).unwrap();
    assert_eq!(load(&paths).unwrap(), s);
    let entries: Vec<_> = std::fs::read_dir(&paths.config_dir).unwrap().map(|e| e.unwrap().file_name()).collect();
    assert_eq!(entries, vec![std::ffi::OsString::from("settings.toml")]);
}

#[test]
fn save_rejects_invalid_settings_without_writing() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    let mut s = Settings::default();
    s.overlay.opacity = 0.0;
    assert_invalid(save(&paths, &s), "overlay.opacity");
    assert!(!paths.settings_file.exists());
}

#[test]
fn save_to_bare_relative_filename() {
    let dir = tempfile::tempdir().unwrap();
    let mut paths = Paths::under(dir.path());
    paths.settings_file = dir.path().join("flat.toml");
    save(&paths, &Settings::default()).unwrap();
    assert_eq!(load(&paths).unwrap(), Settings::default());
}

#[test]
fn save_fails_when_config_dir_is_a_file() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    std::fs::write(&paths.config_dir, "not a dir").unwrap();
    let err = save(&paths, &Settings::default()).unwrap_err();
    assert!(matches!(&err, ConfigError::Io { path, .. } if path == &paths.config_dir), "{err:?}");
}

#[test]
fn save_fails_when_target_is_a_directory() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    std::fs::create_dir_all(paths.settings_file.join("occupied")).unwrap();
    let err = save(&paths, &Settings::default()).unwrap_err();
    assert!(matches!(&err, ConfigError::Io { path, .. } if path == &paths.settings_file), "{err:?}");
    // The temp file was cleaned up.
    assert_eq!(std::fs::read_dir(&paths.config_dir).unwrap().count(), 1);
}

#[test]
fn load_unreadable_path_is_io_error() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    std::fs::create_dir_all(&paths.settings_file).unwrap();
    let err = load(&paths).unwrap_err();
    assert!(matches!(&err, ConfigError::Io { path, .. } if path == &paths.settings_file), "{err:?}");
    assert!(err.to_string().contains("settings.toml"));
}

#[test]
fn load_bad_toml_is_parse_error() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    write_settings(&paths, "version = = 1");
    assert!(matches!(load(&paths), Err(ConfigError::Parse(_))));
    write_settings(&paths, "version = 1\n[overlay]\nopacity = \"high\"\n");
    assert!(matches!(load(&paths), Err(ConfigError::Parse(_))));
}

#[test]
fn load_too_new_is_rejected() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    write_settings(&paths, "version = 99\n");
    assert!(matches!(load(&paths), Err(ConfigError::TooNew { found: 99 })));
}

#[test]
fn load_runs_validation() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    write_settings(&paths, "version = 1\n[overlay]\nauto_hide_ms = 60000\n");
    assert!(matches!(load(&paths), Err(ConfigError::Invalid { .. })));
}

#[test]
fn load_migrates_legacy_file() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    write_settings(&paths, &fixture("v0_full.toml"));
    let s = load(&paths).unwrap();
    assert_eq!(s.version, SETTINGS_VERSION);
    assert_eq!(s.league.league.as_deref(), Some("Rise of the Abyssal"));
    assert!(!s.trade.online_only);
}

// ---------------------------------------------------------------- proptest

fn chord_strategy() -> impl Strategy<Value = Chord> {
    let modifier =
        prop_oneof![Just(KeyModifier::Ctrl), Just(KeyModifier::Alt), Just(KeyModifier::Shift), Just(KeyModifier::Meta)];
    (prop::collection::vec(modifier, 0..4), "Key[A-Z]|F[1-9]|Digit[0-9]")
        .prop_map(|(mods, key)| Chord { modifiers: mods, key })
}

fn settings_strategy() -> impl Strategy<Value = Settings> {
    let league = (any::<bool>(), prop::option::of("[A-Za-z][A-Za-z0-9 \"'\\\\]{0,20}"));
    let hotkeys = (chord_strategy(), chord_strategy(), chord_strategy(), any::<bool>());
    let trade = (
        prop_oneof![Just(PriceSourceKind::Trade), Just(PriceSourceKind::PoeNinja), Just(PriceSourceKind::Poe2Scout)],
        any::<bool>(),
        0.0..=100.0f64,
        any::<bool>(),
        prop::option::of("[a-z0-9.+]{1,8}@[a-z]{1,8}\\.[a-z]{2,4}"),
        prop::option::of("\\PC{0,32}"),
    );
    let overlay = (
        0u32..=10_000,
        0.1..=1.0f64,
        prop_oneof![Just(OverlaySide::Cursor), Just(OverlaySide::Left), Just(OverlaySide::Right)],
    );
    let ui = (
        "[a-z]{2}(-[A-Z]{2})?",
        prop_oneof![Just(Theme::System), Just(Theme::Dark), Just(Theme::Light)],
        any::<bool>(),
        any::<bool>(),
        any::<bool>(),
    );
    let platform = prop::option::of(prop_oneof![
        Just(BackendKind::Windows),
        Just(BackendKind::Macos),
        Just(BackendKind::X11),
        Just(BackendKind::Wayland),
        Just(BackendKind::Fake)
    ]);
    (league, hotkeys, trade, overlay, ui, platform)
        .prop_map(|(league, hotkeys, trade, overlay, ui, platform)| {
            let mut s = Settings::default();
            (s.league.auto_detect, s.league.league) = league;
            (s.hotkeys.price_check, s.hotkeys.price_check_pinned, s.hotkeys.open_settings, s.hotkeys.advanced_copy) =
                hotkeys;
            (
                s.trade.source,
                s.trade.online_only,
                s.trade.widen_pct,
                s.trade.enable_all_stats,
                s.trade.contact,
                s.trade.poesessid,
            ) = trade;
            (s.overlay.auto_hide_ms, s.overlay.opacity, s.overlay.side) = overlay;
            (s.ui.language, s.ui.theme, s.ui.start_minimised, s.ui.check_updates, s.ui.onboarding_done) = ui;
            s.platform.backend_override = platform;
            s
        })
        .prop_filter("must be valid", |s| validate(s).is_ok())
}

proptest! {
    #![proptest_config(ProptestConfig { cases: 64, ..ProptestConfig::default() })]

    #[test]
    fn any_valid_settings_roundtrip_through_save_load(s in settings_strategy()) {
        let dir = tempfile::tempdir().unwrap();
        let paths = Paths::under(dir.path());
        save(&paths, &s).unwrap();
        prop_assert_eq!(load(&paths).unwrap(), s);
    }
}

#[test]
fn rejects_header_unsafe_secrets_and_contacts() {
    let mut s = Settings::default();
    s.trade.poesessid = Some("abc;def".into());
    assert!(matches!(validate(&s), Err(ConfigError::Invalid { field, .. }) if field == "trade.poesessid"));
    s.trade.poesessid = Some("0123abcdef".into());
    assert!(validate(&s).is_ok());
    s.trade.contact = Some("me@example.com;x".into());
    assert!(matches!(validate(&s), Err(ConfigError::Invalid { field, .. }) if field == "trade.contact"));
    s.trade.contact = Some("me@exa\u{7}mple.com".into());
    assert!(validate(&s).is_err());
}

#[test]
fn save_normalises_version_and_restricts_permissions() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    let s = Settings { version: 0, ..Settings::default() };
    save(&paths, &s).unwrap();
    assert_eq!(load(&paths).unwrap().version, rkl_config::SETTINGS_VERSION);
    let text = std::fs::read_to_string(&paths.settings_file).unwrap();
    assert!(text.contains(&format!("version = {}", rkl_config::SETTINGS_VERSION)));
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = std::fs::metadata(&paths.settings_file).unwrap().permissions().mode();
        assert_eq!(mode & 0o777, 0o600);
    }
}

#[test]
fn concurrent_saves_never_corrupt() {
    let dir = tempfile::tempdir().unwrap();
    let paths = std::sync::Arc::new(Paths::under(dir.path()));
    let handles: Vec<_> = (0..8)
        .map(|i| {
            let paths = paths.clone();
            std::thread::spawn(move || {
                let mut s = Settings::default();
                s.trade.widen_pct = f64::from(i);
                for _ in 0..20 {
                    save(&paths, &s).unwrap();
                }
            })
        })
        .collect();
    for h in handles {
        h.join().unwrap();
    }
    assert!(load(&paths).is_ok());
}

#[test]
fn hotkeys_can_be_cleared_and_stay_cleared() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(dir.path());
    let mut s = Settings::default();
    assert_eq!(s.hotkeys.open_settings, Chord::new(&[KeyModifier::Ctrl], "Space"));
    s.hotkeys.open_settings = Chord::unbound();
    s.hotkeys.price_check_pinned = Chord::unbound(); // two cleared hotkeys are not duplicates
    validate(&s).unwrap();
    save(&paths, &s).unwrap();
    let loaded = load(&paths).unwrap();
    assert!(loaded.hotkeys.open_settings.is_unbound(), "cleared hotkey must not reset to the default");
    assert!(loaded.hotkeys.price_check_pinned.is_unbound());
    // Modifiers without a key are still rejected.
    s.hotkeys.open_settings = Chord { modifiers: vec![KeyModifier::Ctrl], key: String::new() };
    assert!(validate(&s).is_err());
}

#[test]
fn legacy_kalandra_settings_are_adopted_once() {
    let dir = tempfile::tempdir().unwrap();
    let paths = Paths::under(&dir.path().join("new"));
    let legacy = dir.path().join("old/settings.toml");
    assert!(!rkl_config::adopt_legacy_settings(&paths, Some(&legacy)).unwrap(), "no legacy file");
    std::fs::create_dir_all(legacy.parent().unwrap()).unwrap();
    std::fs::write(&legacy, "version = 1\n\n[trade]\nwiden_pct = 17.0\n").unwrap();
    assert!(rkl_config::adopt_legacy_settings(&paths, Some(&legacy)).unwrap());
    assert!((load(&paths).unwrap().trade.widen_pct - 17.0).abs() < f64::EPSILON);
    assert!(legacy.exists(), "old file is kept");
    // Never overwrites settings that already exist at the new location.
    std::fs::write(&legacy, "version = 1\n\n[trade]\nwiden_pct = 3.0\n").unwrap();
    assert!(!rkl_config::adopt_legacy_settings(&paths, Some(&legacy)).unwrap());
    assert!((load(&paths).unwrap().trade.widen_pct - 17.0).abs() < f64::EPSILON);
    assert!(!rkl_config::adopt_legacy_settings(&paths, None).unwrap());
    assert!(rkl_config::legacy_settings_file().is_none_or(|p| p.ends_with("settings.toml")));
}
