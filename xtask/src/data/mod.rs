//! `cargo xtask data refresh [--offline]`: download upstream game data, normalise it into
//! `kalandra_core::catalog` records and write `crates/data/assets/`.
//!
//! Sources are documented in `docs/research/data-sources.md`.
use std::{
    collections::HashMap,
    path::{Path, PathBuf},
    time::Duration,
};

use anyhow::Context as _;
use kalandra_data::{AssetSources, Bundle};

pub mod normalise;

/// Descriptive UA as GGG asks for API consumers.
const USER_AGENT: &str = "kalandra-data-refresh/0.1 (+https://github.com/nightah/kalandra)";
/// Pause between requests to the same host, to stay well under rate limits.
const POLITE_DELAY: Duration = Duration::from_millis(1500);

const TRADE2: &str = "https://www.pathofexile.com/api/trade2/data";
const REPOE: &str = "https://repoe-fork.github.io/poe2";

/// One upstream download, cached as `target/data-cache/<cache>`.
#[derive(Debug, Clone, Copy)]
struct Source {
    cache: &'static str,
    base: &'static str,
    path: &'static str,
}

impl Source {
    fn url(&self) -> String {
        format!("{}/{}", self.base, self.path)
    }
}

const TRADE_STATS: Source = Source { cache: "trade2-stats.json", base: TRADE2, path: "stats" };
const TRADE_ITEMS: Source = Source { cache: "trade2-items.json", base: TRADE2, path: "items" };
const TRADE_STATIC: Source = Source { cache: "trade2-static.json", base: TRADE2, path: "static" };
const REPOE_BASES: Source = Source { cache: "repoe-base_items.min.json", base: REPOE, path: "base_items.min.json" };
const REPOE_CLASSES: Source =
    Source { cache: "repoe-item_classes.min.json", base: REPOE, path: "item_classes.min.json" };
const REPOE_INDEX: Source = Source { cache: "repoe-index.html", base: REPOE, path: "" };

const ALL: [Source; 6] = [TRADE_STATS, TRADE_ITEMS, TRADE_STATIC, REPOE_BASES, REPOE_CLASSES, REPOE_INDEX];

fn root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).parent().expect("workspace root").to_path_buf()
}

/// Download every source into `cache_dir` (unless `offline`) and return their contents.
fn fetch_all(cache_dir: &Path, offline: bool) -> anyhow::Result<HashMap<&'static str, String>> {
    std::fs::create_dir_all(cache_dir)?;
    let client = if offline {
        None
    } else {
        Some(reqwest::blocking::Client::builder().user_agent(USER_AGENT).timeout(Duration::from_secs(60)).build()?)
    };
    let mut out = HashMap::new();
    for (i, src) in ALL.iter().enumerate() {
        let path = cache_dir.join(src.cache);
        if let Some(client) = &client {
            if i > 0 {
                std::thread::sleep(POLITE_DELAY);
            }
            eprintln!("fetch {}", src.url());
            let body = client.get(src.url()).send()?.error_for_status()?.text()?;
            std::fs::write(&path, &body)?;
        }
        let body = std::fs::read_to_string(&path)
            .with_context(|| format!("missing cached download {} (run without --offline first)", path.display()))?;
        out.insert(src.cache, body);
    }
    Ok(out)
}

/// Normalise cached downloads into a bundle.
fn build_bundle(raw: &HashMap<&'static str, String>) -> anyhow::Result<Bundle> {
    let repoe_bases: HashMap<String, normalise::RepoeBase> =
        serde_json::from_str(&raw[REPOE_BASES.cache]).context("parsing RePoE base_items")?;
    let repoe_classes: HashMap<String, normalise::RepoeItemClass> =
        serde_json::from_str(&raw[REPOE_CLASSES.cache]).context("parsing RePoE item_classes")?;
    Ok(Bundle {
        stats: normalise::stats(&raw[TRADE_STATS.cache])?,
        bases: normalise::bases(&raw[TRADE_ITEMS.cache], &repoe_bases, &repoe_classes)?,
        uniques: normalise::uniques(&raw[TRADE_ITEMS.cache])?,
        statics: normalise::statics(&raw[TRADE_STATIC.cache])?,
    })
}

fn sources() -> AssetSources {
    AssetSources {
        stats: vec![TRADE_STATS.url()],
        bases: vec![TRADE_ITEMS.url(), REPOE_BASES.url(), REPOE_CLASSES.url()],
        uniques: vec![TRADE_ITEMS.url()],
        statics: vec![TRADE_STATIC.url()],
    }
}

pub fn refresh(offline: bool) -> anyhow::Result<()> {
    let root = root();
    let raw = fetch_all(&root.join("target/data-cache"), offline)?;
    let bundle = build_bundle(&raw)?;
    let generated_at = time::OffsetDateTime::now_utc().format(&time::format_description::well_known::Rfc3339)?;
    let game_version = normalise::game_version(&raw[REPOE_INDEX.cache]);
    let assets = root.join("crates/data/assets");
    let manifest = kalandra_data::write_dir(&assets, &bundle, &sources(), generated_at, game_version)?;
    // Round-trip what we wrote so a broken asset never gets committed.
    kalandra_data::load_dir(&assets).context("re-loading freshly written assets")?;
    eprintln!(
        "wrote {}: {} stats, {} bases, {} uniques, {} statics (game version {})",
        assets.display(),
        manifest.stats.records,
        manifest.bases.records,
        manifest.uniques.records,
        manifest.statics.records,
        manifest.game_version.as_deref().unwrap_or("unknown"),
    );
    corpus_check(&root);
    Ok(())
}

/// Run the parser/matcher corpus tests against the new data.
///
/// Phase 2: the parser may still be `todo!()`, so a failure is reported loudly but does not
/// fail the refresh. Review the output before committing regenerated assets.
fn corpus_check(root: &Path) {
    let sh = match xshell::Shell::new() {
        Ok(sh) => sh,
        Err(e) => {
            eprintln!("warning: corpus check skipped: {e}");
            return;
        }
    };
    sh.change_dir(root);
    eprintln!("running parser corpus check (cargo nextest run -p kalandra-core)");
    match xshell::cmd!(sh, "cargo nextest run -p kalandra-core").run() {
        Ok(()) => eprintln!("corpus check passed"),
        Err(e) => eprintln!(
            "warning: corpus check FAILED ({e}).\n\
             This is expected while kalandra-core's parser is unimplemented (Phase 2); \
             otherwise inspect the failures before committing the new assets."
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn source_urls() {
        assert_eq!(TRADE_STATS.url(), "https://www.pathofexile.com/api/trade2/data/stats");
        assert_eq!(REPOE_INDEX.url(), "https://repoe-fork.github.io/poe2/");
        assert!(USER_AGENT.contains("kalandra"));
    }

    #[test]
    fn offline_without_cache_errors() {
        let dir = tempfile::tempdir().unwrap();
        let err = fetch_all(dir.path(), true).unwrap_err().to_string();
        assert!(err.contains("--offline"), "{err}");
    }

    #[test]
    fn build_bundle_from_cached_samples() {
        let dir = tempfile::tempdir().unwrap();
        let files = [
            (
                TRADE_STATS.cache,
                r##"{"result":[{"id":"explicit","entries":[{"id":"explicit.a","text":"# to Spirit"}]}]}"##,
            ),
            (TRADE_ITEMS.cache, r#"{"result":[{"id":"armour","label":"Armour","entries":[{"type":"Hunter Hood"}]}]}"#),
            (TRADE_STATIC.cache, r#"{"result":[{"id":"Currency","entries":[{"id":"divine","text":"Divine Orb"}]}]}"#),
            (REPOE_BASES.cache, r#"{"x":{"name":"Hunter Hood","item_class":"Helmet","release_state":"released"}}"#),
            (REPOE_CLASSES.cache, r#"{"Helmet":{"name":"Helmets"}}"#),
            (REPOE_INDEX.cache, "PoE2 version 0.5.5.5"),
        ];
        for (name, body) in files {
            std::fs::write(dir.path().join(name), body).unwrap();
        }
        let raw = fetch_all(dir.path(), true).unwrap();
        let bundle = build_bundle(&raw).unwrap();
        assert_eq!(bundle.stats.len(), 1);
        assert_eq!(bundle.bases[0].category, "armour.helmet");
        assert_eq!(bundle.statics[0].text, "Divine Orb");
        assert!(bundle.uniques.is_empty());
        assert_eq!(sources().bases.len(), 3);
    }
}
