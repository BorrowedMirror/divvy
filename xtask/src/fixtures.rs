//! `cargo xtask fixtures record`: re-records the trade2 wiremock fixtures in
//! `crates/trade/tests/fixtures/` from the live API, slowly (default 2 s between requests, well
//! under the published rate limits). Used by `.github/workflows/fixtures-refresh.yml`, whose
//! output is reviewed as a pull request; workstream B's tests decide which files they consume.
//!
//! Recorded files: `leagues.json`, `data_{stats,items,static}.json`, `search.json` (one search
//! for a common unique) and `fetch.json` (the first `fetch_count` ids of that search).
use std::{path::PathBuf, thread, time::Duration};

use anyhow::Context as _;

use crate::commit_config::root;

const BASE: &str = "https://www.pathofexile.com";

#[derive(Debug, clap::Subcommand)]
pub enum FixturesCmd {
    /// Re-record trade fixtures from the live API.
    Record(RecordArgs),
}

#[derive(Debug, clap::Args)]
pub struct RecordArgs {
    /// League to search in; defaults to the first PC league returned by the API.
    #[arg(long)]
    pub league: Option<String>,
    /// Output directory (default: crates/trade/tests/fixtures).
    #[arg(long)]
    pub out: Option<PathBuf>,
    /// Delay between requests in milliseconds.
    #[arg(long, default_value_t = 2000)]
    pub delay_ms: u64,
    /// Number of listings to fetch (trade2 caps this at 10).
    #[arg(long, default_value_t = 10)]
    pub fetch_count: usize,
    /// Base name of the unique to search for.
    #[arg(long, default_value = "Leather Belt")]
    pub base_type: String,
}

pub fn user_agent() -> String {
    format!("rkl-xtask/{} (fixture recorder; +https://github.com/nightah/rust-king-ledger)", env!("CARGO_PKG_VERSION"))
}

/// The trade2 search body recorded into `search.json`.
pub fn search_body(base_type: &str) -> serde_json::Value {
    serde_json::json!({
        "query": {
            "status": { "option": "online" },
            "type": base_type,
            "stats": [{ "type": "and", "filters": [] }]
        },
        "sort": { "price": "asc" }
    })
}

/// Picks the first non-private league (trade2 `realm == "poe2"` when present).
pub fn pick_league(leagues: &serde_json::Value) -> Option<String> {
    leagues["result"]
        .as_array()?
        .iter()
        .find(|l| l.get("realm").and_then(|r| r.as_str()).is_none_or(|r| r == "poe2"))
        .and_then(|l| l["id"].as_str())
        .map(str::to_owned)
}

pub fn fetch_path(search: &serde_json::Value, count: usize) -> anyhow::Result<String> {
    let id = search["id"].as_str().context("search response without `id`")?;
    let ids: Vec<&str> = search["result"]
        .as_array()
        .context("search response without `result`")?
        .iter()
        .filter_map(|v| v.as_str())
        .take(count.min(10))
        .collect();
    anyhow::ensure!(!ids.is_empty(), "search returned no results; pick another --base-type");
    Ok(format!("/api/trade2/fetch/{}?query={id}", ids.join(",")))
}

pub fn run(cmd: &FixturesCmd) -> anyhow::Result<()> {
    let FixturesCmd::Record(args) = cmd;
    let out = args.out.clone().unwrap_or_else(|| root().join("crates/trade/tests/fixtures"));
    std::fs::create_dir_all(&out)?;
    let client = reqwest::blocking::Client::builder().user_agent(user_agent()).build()?;
    let delay = Duration::from_millis(args.delay_ms);
    let mut first = true;
    let mut get = |path: &str, body: Option<&serde_json::Value>| -> anyhow::Result<serde_json::Value> {
        if !std::mem::take(&mut first) {
            thread::sleep(delay);
        }
        let url = format!("{BASE}{path}");
        eprintln!("-> {url}");
        let req = match body {
            Some(b) => client.post(&url).json(b),
            None => client.get(&url),
        };
        let resp = req.send()?;
        let status = resp.status();
        anyhow::ensure!(status.is_success(), "{url}: HTTP {status}: {}", resp.text().unwrap_or_default());
        Ok(resp.json()?)
    };
    let write = |name: &str, v: &serde_json::Value| -> anyhow::Result<()> {
        std::fs::write(out.join(name), serde_json::to_string_pretty(v)? + "\n")?;
        Ok(())
    };

    let leagues = get("/api/trade2/data/leagues", None)?;
    write("leagues.json", &leagues)?;
    for which in ["stats", "items", "static"] {
        write(&format!("data_{which}.json"), &get(&format!("/api/trade2/data/{which}"), None)?)?;
    }
    let league = match &args.league {
        Some(l) => l.clone(),
        None => pick_league(&leagues).context("no league in leagues response")?,
    };
    let search = get(&format!("/api/trade2/search/poe2/{}", url_escape(&league)), Some(&search_body(&args.base_type)))?;
    write("search.json", &search)?;
    write("fetch.json", &get(&fetch_path(&search, args.fetch_count)?, None)?)?;
    eprintln!("recorded fixtures for league `{league}` into {}", out.display());
    Ok(())
}

/// Percent-encodes a league name for use as a path segment.
pub fn url_escape(s: &str) -> String {
    s.bytes()
        .map(|b| match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => (b as char).to_string(),
            _ => format!("%{b:02X}"),
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn picks_poe2_league() {
        let leagues = serde_json::json!({"result": [
            {"id": "Standard", "realm": "pc"},
            {"id": "Dawn of the Hunt", "realm": "poe2"}
        ]});
        assert_eq!(pick_league(&leagues).as_deref(), Some("Dawn of the Hunt"));
        assert_eq!(pick_league(&serde_json::json!({"result": [{"id": "Standard"}]})).as_deref(), Some("Standard"));
        assert_eq!(pick_league(&serde_json::json!({})), None);
    }

    #[test]
    fn builds_fetch_path() {
        let search = serde_json::json!({"id": "Q1", "result": ["a", "b", "c"], "total": 3});
        assert_eq!(fetch_path(&search, 2).unwrap(), "/api/trade2/fetch/a,b?query=Q1");
        assert_eq!(fetch_path(&search, 50).unwrap(), "/api/trade2/fetch/a,b,c?query=Q1");
        assert!(fetch_path(&serde_json::json!({"id": "Q", "result": []}), 10).is_err());
        assert!(fetch_path(&serde_json::json!({"result": ["a"]}), 10).is_err());
    }

    #[test]
    fn escapes_and_describes() {
        assert_eq!(url_escape("Dawn of the Hunt"), "Dawn%20of%20the%20Hunt");
        assert_eq!(url_escape("HC_SSF-1.0~"), "HC_SSF-1.0~");
        assert!(user_agent().contains("github.com/nightah/rust-king-ledger"));
        assert_eq!(search_body("Leather Belt")["query"]["type"], "Leather Belt");
    }
}
