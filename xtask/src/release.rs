//! `cargo xtask release <version>`: bump every version source, then create a
//! `release: v<version>` commit and an annotated `v<version>` tag. It never pushes: pushing the
//! tag is what triggers `.github/workflows/release.yml`, where GitHub writes the release notes
//! from the labelled pull requests (`.github/release.yml`).
use std::fmt::Write as _;

use anyhow::{Context as _, bail, ensure};
use xshell::{Shell, cmd};

use crate::commit_config::root;

#[derive(Debug, clap::Args)]
pub struct ReleaseArgs {
    /// New version, e.g. `0.2.0` or `0.2.0-rc.1` (no leading `v`).
    pub version: String,
    /// Only print what would change.
    #[arg(long)]
    pub dry_run: bool,
    /// Allow a dirty working tree.
    #[arg(long)]
    pub allow_dirty: bool,
}

const CARGO_TOML: &str = "Cargo.toml";
const JSON_FILES: &[&str] = &["apps/desktop/src-tauri/tauri.conf.json", "web/package.json"];

/// Accepts `MAJOR.MINOR.PATCH` with an optional `-prerelease` of dot-separated `[0-9A-Za-z-]`.
pub fn validate_version(v: &str) -> anyhow::Result<()> {
    let (core, pre) = v.split_once('-').map_or((v, None), |(c, p)| (c, Some(p)));
    let parts: Vec<_> = core.split('.').collect();
    ensure!(parts.len() == 3, "version `{v}` must be MAJOR.MINOR.PATCH[-PRE]");
    for p in parts {
        ensure!(!p.is_empty() && p.bytes().all(|b| b.is_ascii_digit()), "version `{v}`: `{p}` is not a number");
        ensure!(p == "0" || !p.starts_with('0'), "version `{v}`: leading zero in `{p}`");
    }
    if let Some(pre) = pre {
        for id in pre.split('.') {
            ensure!(
                !id.is_empty() && id.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-'),
                "version `{v}`: bad pre-release identifier `{id}`"
            );
        }
    }
    Ok(())
}

/// Rewrites `version = "..."` inside `[workspace.package]`, preserving everything else.
pub fn bump_workspace_version(cargo_toml: &str, new: &str) -> anyhow::Result<String> {
    let mut out = String::with_capacity(cargo_toml.len());
    let mut in_section = false;
    let mut done = false;
    for line in cargo_toml.split_inclusive('\n') {
        let trimmed = line.trim();
        if trimmed.starts_with('[') {
            in_section = trimmed == "[workspace.package]";
        }
        if in_section && !done && trimmed.starts_with("version") && trimmed[7..].trim_start().starts_with('=') {
            let eol = if line.ends_with("\r\n") {
                "\r\n"
            } else if line.ends_with('\n') {
                "\n"
            } else {
                ""
            };
            let _ = write!(out, "version = \"{new}\"{eol}");
            done = true;
        } else {
            out.push_str(line);
        }
    }
    ensure!(done, "no `version` key in [workspace.package]");
    Ok(out)
}

/// Rewrites the top-level `"version"` of a JSON document textually (keeps formatting/key order).
pub fn bump_json_version(text: &str, new: &str) -> anyhow::Result<String> {
    let value: serde_json::Value = serde_json::from_str(text)?;
    let old = value.get("version").and_then(|v| v.as_str()).context("no top-level string `version`")?;
    let needle = format!("\"{old}\"");
    // The first `"version"` key in the file is the top-level one in our manifests; verify below.
    let key = text.find("\"version\"").context("no `\"version\"` key")?;
    let at = key + text[key..].find(&needle).context("version value not found")?;
    let out = format!("{}\"{new}\"{}", &text[..at], &text[at + needle.len()..]);
    let check: serde_json::Value = serde_json::from_str(&out)?;
    if check.get("version").and_then(|v| v.as_str()) != Some(new) {
        bail!("first `version` key is not the top-level one; edit by hand");
    }
    Ok(out)
}

pub fn run(args: &ReleaseArgs) -> anyhow::Result<()> {
    validate_version(&args.version)?;
    let version = args.version.as_str();
    let tag = format!("v{version}");
    let sh = Shell::new()?;
    let root = root();
    sh.change_dir(&root);

    if !args.allow_dirty && !cmd!(sh, "git status --porcelain").read()?.trim().is_empty() {
        bail!("working tree is dirty; commit or stash first (or pass --allow-dirty)");
    }
    let existing = cmd!(sh, "git rev-parse -q --verify refs/tags/{tag}").quiet().ignore_status().read()?;
    if !existing.trim().is_empty() {
        bail!("tag {tag} already exists");
    }

    let mut edits = vec![(CARGO_TOML, bump_workspace_version(&sh.read_file(CARGO_TOML)?, version)?)];
    for f in JSON_FILES {
        edits.push((f, bump_json_version(&sh.read_file(f)?, version)?));
    }
    if args.dry_run {
        for (f, _) in &edits {
            println!("would set version {version} in {f}");
        }
        println!("would run: git commit -m 'release: {tag}'; git tag -a {tag}");
        return Ok(());
    }
    for (f, body) in &edits {
        sh.write_file(f, body)?;
    }
    // Refresh the workspace members' entries in Cargo.lock only.
    cmd!(sh, "cargo update --workspace --offline").run()?;

    let files: Vec<&str> = edits.iter().map(|(f, _)| *f).chain(["Cargo.lock"]).collect();
    cmd!(sh, "git add {files...}").run()?;
    let msg = format!("release: {tag}");
    cmd!(sh, "git commit -m {msg}").run()?;
    cmd!(sh, "git tag -a {tag} -m {tag}").run()?;
    println!("created commit and tag {tag}. Review, then: git push origin master {tag}");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_versions() {
        for ok in ["0.1.0", "1.20.3", "0.1.0-rc.1", "2.0.0-beta-2.x"] {
            validate_version(ok).unwrap_or_else(|e| panic!("{ok}: {e}"));
        }
        for bad in ["v0.1.0", "0.1", "0.1.0.0", "01.2.3", "1.2.x", "1.2.3-", "1.2.3-a..b", "1.2.3-a_b", ""] {
            assert!(validate_version(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn bumps_only_workspace_package_version() {
        let toml = "[package]\nversion = \"9.9.9\"\n\n[workspace.package]\nversion   = \"0.1.0-rc.1\"\nedition = \"2024\"\n\n[workspace.dependencies]\nfoo = { version = \"1\" }\n";
        let out = bump_workspace_version(toml, "0.2.0").unwrap();
        assert_eq!(out, toml.replace("version   = \"0.1.0-rc.1\"", "version = \"0.2.0\""));
        assert!(out.contains("version = \"9.9.9\""));
        assert!(out.contains("foo = { version = \"1\" }"));
    }

    #[test]
    fn keeps_crlf_and_rejects_missing_key() {
        let toml = "[workspace.package]\r\nversion = \"1.0.0\"\r\nedition = \"2024\"\r\n";
        assert_eq!(bump_workspace_version(toml, "1.0.1").unwrap(), toml.replace("1.0.0", "1.0.1"));
        assert!(bump_workspace_version("[workspace.package]\nedition = \"2024\"\n", "1.0.0").is_err());
        assert!(bump_workspace_version("[package]\nversion = \"1\"\n", "1.0.0").is_err());
        // `versions = ...` is not `version`.
        assert!(bump_workspace_version("[workspace.package]\nversions = \"1\"\n", "1.0.0").is_err());
    }

    #[test]
    fn bumps_json_top_level_version() {
        let json = "{\n  \"name\": \"rkl-web\",\n  \"version\": \"0.1.0-rc.1\",\n  \"dependencies\": {\n    \"x\": \"0.1.0-rc.1\"\n  }\n}\n";
        let out = bump_json_version(json, "0.2.0").unwrap();
        assert_eq!(out, json.replacen("\"0.1.0-rc.1\"", "\"0.2.0\"", 1));
        assert!(out.contains("\"x\": \"0.1.0-rc.1\""));
    }

    #[test]
    fn json_bump_refuses_nested_first_version() {
        let json = r#"{"plugins": {"version": "1.0.0"}, "version": "1.0.0"}"#;
        assert!(bump_json_version(json, "2.0.0").is_err());
        assert!(bump_json_version(r#"{"name": "x"}"#, "2.0.0").is_err());
        assert!(bump_json_version("not json", "2.0.0").is_err());
    }

    #[test]
    fn bumps_real_manifests() {
        let root = root();
        let cargo = std::fs::read_to_string(root.join(CARGO_TOML)).unwrap();
        assert!(bump_workspace_version(&cargo, "9.8.7").unwrap().contains("version = \"9.8.7\""));
        for f in JSON_FILES {
            let text = std::fs::read_to_string(root.join(f)).unwrap();
            let out: serde_json::Value = serde_json::from_str(&bump_json_version(&text, "9.8.7").unwrap()).unwrap();
            assert_eq!(out["version"], "9.8.7", "{f}");
        }
    }
}
