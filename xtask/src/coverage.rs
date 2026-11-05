//! `cargo xtask coverage [--check]`: one instrumented nextest run for the workspace, then a
//! per-crate summary, per-crate lcov files (`target/coverage/<crate>.lcov`, uploaded to Codecov
//! with one flag per crate) and line-coverage gates from CONTRACTS.md.
use std::{collections::BTreeMap, fmt, path::Path};

use anyhow::Context as _;
use xshell::{Shell, cmd};

use crate::commit_config::root;

/// Workspace path prefix -> short crate name (also the Codecov flag).
pub const CRATES: &[(&str, &str)] = &[
    ("crates/core/", "core"),
    ("crates/data/", "data"),
    ("crates/trade/", "trade"),
    ("crates/platform/", "platform"),
    ("crates/config/", "config"),
    ("crates/testkit/", "testkit"),
    ("apps/desktop/", "desktop"),
];

/// Minimum line coverage (percent) per crate.
pub const GATES: &[(&str, f64)] =
    &[("core", 90.0), ("trade", 90.0), ("data", 90.0), ("config", 90.0), ("platform", 70.0)];

/// Packages never instrumented: tooling, not product code.
const EXCLUDED_PACKAGES: &[&str] = &["xtask", "fake-game"];

#[derive(Debug, clap::Args)]
pub struct CoverageArgs {
    /// Fail when a crate is below its gate.
    #[arg(long)]
    pub check: bool,
    /// Reuse the profile data of a previous run instead of running the tests again.
    #[arg(long)]
    pub no_run: bool,
    /// nextest profile (see .config/nextest.toml).
    #[arg(long, default_value = "default")]
    pub profile: String,
}

#[derive(Debug, Default, Clone, Copy, PartialEq, Eq)]
pub struct Lines {
    pub covered: u64,
    pub count: u64,
}

impl Lines {
    pub fn percent(self) -> f64 {
        if self.count == 0 { 0.0 } else { self.covered as f64 * 100.0 / self.count as f64 }
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct GateResult {
    pub krate: String,
    pub lines: Option<Lines>,
    pub min: f64,
}

impl GateResult {
    pub fn passed(&self) -> bool {
        self.lines.is_some_and(|l| l.count > 0 && l.percent() + f64::EPSILON >= self.min)
    }
}

impl fmt::Display for GateResult {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let verdict = if self.passed() { "ok" } else { "FAIL" };
        match self.lines {
            Some(l) => write!(
                f,
                "{verdict:>4}  {:<10} {:>6.2}% (>= {:.0}%)  {}/{} lines",
                self.krate,
                l.percent(),
                self.min,
                l.covered,
                l.count
            ),
            None => write!(f, "{verdict:>4}  {:<10} no coverage data (>= {:.0}%)", self.krate, self.min),
        }
    }
}

fn normalise(path: &str) -> String {
    path.replace('\\', "/")
}

/// Maps a source path (absolute or workspace-relative, any separator) to its crate name.
pub fn crate_for(path: &str, root: &str) -> Option<&'static str> {
    let path = normalise(path);
    let root = normalise(root);
    let root = root.trim_end_matches('/');
    let rel = path.strip_prefix(root).map_or(path.as_str(), |r| r.trim_start_matches('/'));
    CRATES.iter().find(|(prefix, _)| rel.starts_with(prefix)).map(|(_, name)| *name)
}

/// Sums line counts per crate from `cargo llvm-cov report --json [--summary-only]` output.
pub fn summarise(json: &serde_json::Value, root: &str) -> anyhow::Result<BTreeMap<String, Lines>> {
    let mut out: BTreeMap<String, Lines> = BTreeMap::new();
    let data = json.get("data").and_then(|d| d.as_array()).context("llvm-cov json: missing `data`")?;
    for export in data {
        let files = export.get("files").and_then(|f| f.as_array()).context("llvm-cov json: missing `files`")?;
        for file in files {
            let name = file.get("filename").and_then(|n| n.as_str()).context("file without `filename`")?;
            let Some(krate) = crate_for(name, root) else { continue };
            let lines = &file["summary"]["lines"];
            let entry = out.entry(krate.to_owned()).or_default();
            entry.covered += lines["covered"].as_u64().unwrap_or(0);
            entry.count += lines["count"].as_u64().unwrap_or(0);
        }
    }
    Ok(out)
}

pub fn evaluate(totals: &BTreeMap<String, Lines>, gates: &[(&str, f64)]) -> Vec<GateResult> {
    gates
        .iter()
        .map(|(krate, min)| GateResult { krate: (*krate).to_owned(), lines: totals.get(*krate).copied(), min: *min })
        .collect()
}

/// Splits a workspace lcov report into one report per crate (records outside known crates dropped).
pub fn split_lcov(lcov: &str, root: &str) -> BTreeMap<String, String> {
    let mut out: BTreeMap<String, String> = BTreeMap::new();
    let mut record = String::new();
    let mut krate: Option<&'static str> = None;
    for line in lcov.lines() {
        if let Some(sf) = line.strip_prefix("SF:") {
            krate = crate_for(sf, root);
        }
        record.push_str(line);
        record.push('\n');
        if line == "end_of_record" {
            if let Some(k) = krate.take() {
                out.entry(k.to_owned()).or_default().push_str(&record);
            }
            record.clear();
        }
    }
    out
}

pub fn run(args: &CoverageArgs) -> anyhow::Result<()> {
    let sh = Shell::new()?;
    let root = root();
    sh.change_dir(&root);
    let out_dir = root.join("target/coverage");
    std::fs::create_dir_all(&out_dir)?;

    if !args.no_run {
        let excludes: Vec<String> =
            EXCLUDED_PACKAGES.iter().flat_map(|p| ["--exclude".to_owned(), (*p).to_owned()]).collect();
        let profile = &args.profile;
        cmd!(sh, "cargo llvm-cov clean --workspace").run()?;
        cmd!(sh, "cargo llvm-cov nextest --workspace {excludes...} --no-report --profile {profile}").run()?;
    }

    let summary_path = out_dir.join("summary.json");
    let lcov_path = out_dir.join("lcov.info");
    cmd!(sh, "cargo llvm-cov report --json --summary-only --output-path {summary_path}").run()?;
    cmd!(sh, "cargo llvm-cov report --lcov --output-path {lcov_path}").run()?;

    let root_str = root.to_string_lossy().into_owned();
    let json: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&summary_path)?)?;
    let totals = summarise(&json, &root_str)?;
    write_split(&out_dir, &std::fs::read_to_string(&lcov_path)?, &root_str)?;

    println!("\nline coverage per crate:");
    for (krate, lines) in &totals {
        println!("      {krate:<10} {:>6.2}%  {}/{} lines", lines.percent(), lines.covered, lines.count);
    }
    println!("\ngates:");
    let results = evaluate(&totals, GATES);
    for r in &results {
        println!("{r}");
    }
    let failed: Vec<_> = results.iter().filter(|r| !r.passed()).map(|r| r.krate.as_str()).collect();
    if args.check && !failed.is_empty() {
        anyhow::bail!("coverage gates failed: {}", failed.join(", "));
    }
    Ok(())
}

fn write_split(out_dir: &Path, lcov: &str, root: &str) -> anyhow::Result<()> {
    for (krate, body) in split_lcov(lcov, root) {
        std::fs::write(out_dir.join(format!("{krate}.lcov")), body)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const ROOT: &str = "/work/kalandra";

    fn sample() -> serde_json::Value {
        serde_json::json!({
            "type": "llvm.coverage.json.export",
            "version": "2.0.1",
            "data": [{
                "files": [
                    {"filename": "/work/kalandra/crates/core/src/lib.rs", "summary": {"lines": {"count": 100, "covered": 95, "percent": 95.0}}},
                    {"filename": "/work/kalandra/crates/core/src/parser.rs", "summary": {"lines": {"count": 100, "covered": 90, "percent": 90.0}}},
                    {"filename": "/work/kalandra/crates/trade/src/lib.rs", "summary": {"lines": {"count": 10, "covered": 8, "percent": 80.0}}},
                    {"filename": "/work/kalandra/crates/platform/src/lib.rs", "summary": {"lines": {"count": 10, "covered": 7, "percent": 70.0}}},
                    {"filename": "/work/kalandra/xtask/src/main.rs", "summary": {"lines": {"count": 50, "covered": 0, "percent": 0.0}}},
                    {"filename": "/home/u/.cargo/registry/src/foo/lib.rs", "summary": {"lines": {"count": 5, "covered": 0, "percent": 0.0}}}
                ],
                "totals": {}
            }]
        })
    }

    #[test]
    fn maps_paths_to_crates() {
        assert_eq!(crate_for("/work/kalandra/crates/core/src/lib.rs", ROOT), Some("core"));
        assert_eq!(crate_for(r"C:\a\b\crates\data\src\lib.rs", r"C:\a\b"), Some("data"));
        assert_eq!(crate_for("crates/config/src/lib.rs", ROOT), Some("config"));
        assert_eq!(crate_for("/work/kalandra/apps/desktop/src-tauri/src/lib.rs", "/work/kalandra/"), Some("desktop"));
        assert_eq!(crate_for("/work/kalandra/xtask/src/main.rs", ROOT), None);
    }

    #[test]
    fn summarises_per_crate() {
        let totals = summarise(&sample(), ROOT).unwrap();
        assert_eq!(totals["core"], Lines { covered: 185, count: 200 });
        assert_eq!(totals["trade"], Lines { covered: 8, count: 10 });
        assert!(!totals.contains_key("xtask"));
        assert_eq!(totals.len(), 3);
    }

    #[test]
    fn evaluates_gates() {
        let totals = summarise(&sample(), ROOT).unwrap();
        let results = evaluate(&totals, GATES);
        let by: BTreeMap<_, _> = results.iter().map(|r| (r.krate.as_str(), r.passed())).collect();
        assert!(by["core"], "92.5% >= 90");
        assert!(!by["trade"], "80% < 90");
        assert!(by["platform"], "70% >= 70 (inclusive)");
        assert!(!by["data"], "missing data fails");
        assert!(!by["config"]);
        let shown = results.iter().map(ToString::to_string).collect::<Vec<_>>().join("\n");
        assert!(shown.contains("FAIL  data       no coverage data"), "{shown}");
        assert!(shown.contains("  ok  core        92.50%"), "{shown}");
    }

    #[test]
    fn empty_crate_fails_gate() {
        let totals = BTreeMap::from([("core".to_owned(), Lines::default())]);
        assert!(!evaluate(&totals, &[("core", 0.0)])[0].passed());
        assert!((Lines::default().percent()).abs() < f64::EPSILON);
    }

    #[test]
    fn rejects_malformed_json() {
        assert!(summarise(&serde_json::json!({}), ROOT).is_err());
        assert!(summarise(&serde_json::json!({"data": [{}]}), ROOT).is_err());
    }

    #[test]
    fn splits_lcov_by_crate() {
        let lcov = "TN:\nSF:/work/kalandra/crates/core/src/lib.rs\nDA:1,1\nend_of_record\n\
                    TN:\nSF:/work/kalandra/xtask/src/main.rs\nDA:1,0\nend_of_record\n\
                    TN:\nSF:/work/kalandra/crates/core/src/parser.rs\nDA:2,0\nend_of_record\n\
                    TN:\nSF:/work/kalandra/crates/trade/src/lib.rs\nDA:3,1\nend_of_record\n";
        let split = split_lcov(lcov, ROOT);
        assert_eq!(split.len(), 2);
        assert_eq!(split["core"].matches("end_of_record").count(), 2);
        assert!(split["trade"].starts_with("TN:\nSF:/work/kalandra/crates/trade/src/lib.rs\n"));
        assert!(!split.values().any(|v| v.contains("xtask")));
    }
}
