//! `cargo xtask windows-installer`: cross-compile the Windows NSIS installer on Linux.
//!
//! `cargo tauri build` cross-compiles with `cargo-xwin` and bundles with the system `makensis`.
//! Distribution packages of NSIS (the Arch AUR one, for one) are often built for the 64-bit
//! target only, while Tauri's installer script needs the 32-bit `x86-unicode` stubs and its own
//! 32-bit plugin. This sets up a copy of the NSIS data directory under `target/nsis-x86/`, adds
//! the 32-bit stubs and plugins from the official NSIS Windows zip when the system copy lacks
//! them, and puts a `makensis` wrapper on `PATH` that selects that target. Tauri sets `NSISDIR`
//! itself when it runs `makensis`, so the wrapper sets it again.
//!
//! The installer is not code-signed. An MSI needs WiX, which only runs on Windows; build that
//! on Windows with `cargo tauri build --bundles msi,nsis` or through the release workflow.
use std::{
    path::{Path, PathBuf},
    time::Duration,
};

use anyhow::{Context as _, bail, ensure};
use sha2::{Digest as _, Sha256};
use xshell::{Shell, cmd};

use crate::commit_config::root;

#[derive(Debug, clap::Args)]
pub struct WindowsArgs {
    /// Extra arguments for `cargo tauri build`, after `--` (e.g. `-- --verbose`).
    #[arg(last = true)]
    pub extra: Vec<String>,
}

const TARGET: &str = "x86_64-pc-windows-msvc";

/// The official NSIS Windows zips this knows the SHA-256 of, by NSIS version. The zip must match
/// the system `makensis` version; add a line here when the system NSIS is upgraded.
const NSIS_ZIPS: &[(&str, &str)] = &[("3.13", "ba63dffc4410ee89193e1cb5a41989991bd77c61068da17e3156d136b7b0b3d8")];

/// Tools that must be on `PATH`, with the Arch package that provides each.
const TOOLS: &[(&str, &str)] = &[
    ("makensis", "nsis (AUR)"),
    ("clang-cl", "clang"),
    ("lld-link", "lld"),
    ("llvm-rc", "llvm"),
    ("cargo-xwin", "`cargo install cargo-xwin --locked`"),
    ("cargo-tauri", "`cargo install tauri-cli --locked`"),
];

pub fn run(args: &WindowsArgs) -> anyhow::Result<()> {
    if !cfg!(target_os = "linux") {
        bail!("this cross-compiles from Linux; on Windows run `cargo tauri build --bundles msi,nsis` in apps/desktop");
    }
    let path = search_path();
    let missing: Vec<String> =
        TOOLS.iter().filter(|(t, _)| find(&path, t).is_none()).map(|(t, from)| format!("  {t}: {from}")).collect();
    ensure!(missing.is_empty(), "missing tools:\n{}", missing.join("\n"));

    let sh = Shell::new()?;
    let system_makensis = find(&path, "makensis").expect("checked above");
    let hdr = cmd!(sh, "{system_makensis} -HDRINFO").ignore_status().read()?;
    let version = nsis_version(&cmd!(sh, "{system_makensis} -VERSION").read()?)?;
    let system_dir = nsis_dir(&hdr).unwrap_or_else(|| PathBuf::from("/usr/share/nsis"));

    let work = root().join("target/nsis-x86").join(&version);
    let wrapper_dir = prepare(&sh, &work, &system_dir, &system_makensis, &version)?;

    let mut new_path = vec![wrapper_dir];
    new_path.extend(path);
    let new_path = std::env::join_paths(new_path)?;
    sh.change_dir(root().join("apps/desktop"));
    let extra = &args.extra;
    cmd!(sh, "cargo tauri build --runner cargo-xwin --target {TARGET} --bundles nsis {extra...}")
        .env("PATH", new_path)
        .run()?;

    let target_dir = std::env::var_os("CARGO_TARGET_DIR").map_or_else(|| root().join("target"), PathBuf::from);
    eprintln!("installer: {}", target_dir.join(TARGET).join("release/bundle/nsis").display());
    Ok(())
}

/// `PATH`, plus cargo's bin directory (where `cargo install` puts `cargo-xwin`).
fn search_path() -> Vec<PathBuf> {
    let mut dirs: Vec<PathBuf> =
        std::env::var_os("PATH").map(|p| std::env::split_paths(&p).collect()).unwrap_or_default();
    let cargo_bin = std::env::var_os("CARGO_HOME")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".cargo")))
        .map(|d| d.join("bin"));
    if let Some(bin) = cargo_bin.filter(|b| !dirs.contains(b)) {
        dirs.push(bin);
    }
    dirs
}

fn find(path: &[PathBuf], name: &str) -> Option<PathBuf> {
    path.iter().map(|d| d.join(name)).find(|p| p.is_file())
}

/// `v3.13` -> `3.13`.
fn nsis_version(out: &str) -> anyhow::Result<String> {
    let v = out.trim().trim_start_matches('v');
    ensure!(
        !v.is_empty() && v.starts_with(|c: char| c.is_ascii_digit()),
        "unexpected `makensis -VERSION` output: {out}"
    );
    Ok(v.to_owned())
}

/// The data directory from `makensis -HDRINFO`'s `NSISDIR=` symbol.
fn nsis_dir(hdrinfo: &str) -> Option<PathBuf> {
    hdrinfo.split([',', '\n']).find_map(|s| s.trim().strip_prefix("NSISDIR=")).map(PathBuf::from)
}

fn has_x86(dir: &Path) -> bool {
    dir.join("Stubs/zlib-x86-unicode").is_file() && dir.join("Plugins/x86-unicode").is_dir()
}

/// Build `<work>/nsis` (the data directory) and `<work>/bin/makensis`; returns `<work>/bin`.
/// Reuses a finished setup.
fn prepare(sh: &Shell, work: &Path, system_dir: &Path, makensis: &Path, version: &str) -> anyhow::Result<PathBuf> {
    let data = work.join("nsis");
    let bin = work.join("bin");
    let wrapper = bin.join("makensis");
    if has_x86(&data) && wrapper.is_file() {
        return Ok(bin);
    }
    if work.exists() {
        std::fs::remove_dir_all(work)?;
    }
    std::fs::create_dir_all(&bin)?;
    cmd!(sh, "cp -rL {system_dir} {data}").run()?;
    if !has_x86(&data) {
        let zip = download_zip(work, version)?;
        let unpacked = work.join("zip");
        cmd!(sh, "unzip -q {zip} -d {unpacked}").run().context("unzip the NSIS zip (is `unzip` installed?)")?;
        let src = unpacked.join(format!("nsis-{version}"));
        for entry in std::fs::read_dir(src.join("Stubs"))? {
            let entry = entry?;
            if entry.file_name().to_string_lossy().contains("-x86-") {
                std::fs::copy(entry.path(), data.join("Stubs").join(entry.file_name()))?;
            }
        }
        let plugins = src.join("Plugins");
        let to = data.join("Plugins");
        cmd!(sh, "cp -r {plugins}/x86-unicode {plugins}/x86-ansi {to}").run()?;
        std::fs::remove_dir_all(&unpacked)?;
        ensure!(has_x86(&data), "the NSIS zip had no x86-unicode stubs");
    }
    std::fs::write(&wrapper, wrapper_script(makensis, &data))?;
    cmd!(sh, "chmod +x {wrapper}").run()?;
    Ok(bin)
}

fn wrapper_script(makensis: &Path, data: &Path) -> String {
    format!(
        "#!/bin/sh\n# Written by `cargo xtask windows-installer`.\nNSISDIR='{}' exec '{}' -X'Target x86-unicode' \"$@\"\n",
        data.display(),
        makensis.display()
    )
}

/// Download `nsis-<version>.zip` from SourceForge into `work` and check it against `NSIS_ZIPS`.
fn download_zip(work: &Path, version: &str) -> anyhow::Result<PathBuf> {
    let Some((_, want)) = NSIS_ZIPS.iter().find(|(v, _)| *v == version) else {
        bail!("no pinned checksum for the NSIS {version} zip; add one to NSIS_ZIPS in xtask/src/windows.rs");
    };
    let url = format!("https://downloads.sourceforge.net/project/nsis/NSIS%203/{version}/nsis-{version}.zip");
    eprintln!("fetch {url}");
    let client = reqwest::blocking::Client::builder()
        .user_agent(crate::fixtures::user_agent())
        .timeout(Duration::from_mins(5))
        .build()?;
    let body = client.get(&url).send()?.error_for_status()?.bytes()?;
    let got = sha256_hex(&body);
    ensure!(got == *want, "{url} has SHA-256 {got}, expected {want}");
    let out = work.join(format!("nsis-{version}.zip"));
    std::fs::write(&out, &body)?;
    Ok(out)
}

fn sha256_hex(bytes: &[u8]) -> String {
    Sha256::digest(bytes).iter().fold(String::new(), |mut s, b| {
        use std::fmt::Write as _;
        let _ = write!(s, "{b:02x}");
        s
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_version_and_data_dir() {
        assert_eq!(nsis_version("v3.13\n").unwrap(), "3.13");
        assert!(nsis_version("oops").is_err());
        let hdr = "Defined symbols: NSIS_AMD64=1,NSIS_CPU=amd64,NSISDIR=/usr/share/nsis,PREFIX_CONF=/etc";
        assert_eq!(nsis_dir(hdr), Some(PathBuf::from("/usr/share/nsis")));
        assert_eq!(nsis_dir("nothing here"), None);
    }

    #[test]
    fn wrapper_sets_the_data_dir_and_target() {
        let s = wrapper_script(Path::new("/usr/bin/makensis"), Path::new("/w/nsis"));
        assert!(s.contains("NSISDIR='/w/nsis' exec '/usr/bin/makensis' -X'Target x86-unicode' \"$@\""));
    }

    #[test]
    fn sha256_is_lower_hex() {
        assert_eq!(sha256_hex(b"abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    }
}
