//! Repo tooling: `cargo xtask <cmd>`.
use clap::{Parser, Subcommand};

mod commit_config;
mod coverage;
mod data;
mod fixtures;
mod lint;
mod release;

#[derive(Debug, Parser)]
#[command(name = "xtask")]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Debug, Subcommand)]
enum Cmd {
    /// Data pipeline (workstream C).
    Data {
        #[command(subcommand)]
        cmd: DataCmd,
    },
    /// Run coverage with per-crate gates; writes target/coverage/<crate>.lcov.
    Coverage(coverage::CoverageArgs),
    /// Generate derived files: .commitlintrc.json, docs/contributing/commit-message.md, bindings.
    Gen {
        /// Skip regenerating web/src/bindings.ts (needs the Tauri system libraries).
        #[arg(long)]
        no_bindings: bool,
    },
    /// Bump versions, regenerate CHANGELOG.md, commit `release: v<version>` and tag (never pushes).
    Release(release::ReleaseArgs),
    /// fmt + clippy + deny + machete + web lint/typecheck.
    Lint(lint::LintArgs),
    /// Trade API fixtures.
    Fixtures {
        #[command(subcommand)]
        cmd: fixtures::FixturesCmd,
    },
}

#[derive(Debug, Subcommand)]
enum DataCmd {
    /// Download trade2 data + PoE2 exports, normalise, write crates/data/assets + manifest.
    Refresh {
        /// Use cached downloads from target/data-cache instead of the network.
        #[arg(long)]
        offline: bool,
    },
}

fn gen_all(no_bindings: bool) -> anyhow::Result<()> {
    commit_config::generate()?;
    if !no_bindings {
        let sh = xshell::Shell::new()?;
        sh.change_dir(commit_config::root());
        xshell::cmd!(sh, "cargo test --quiet -p kalandra-desktop --test bindings")
            .env("KALANDRA_UPDATE_BINDINGS", "1")
            .run()?;
    }
    Ok(())
}

fn main() -> anyhow::Result<()> {
    match Cli::parse().cmd {
        Cmd::Gen { no_bindings } => gen_all(no_bindings),
        Cmd::Data { cmd: DataCmd::Refresh { offline } } => data::refresh(offline),
        Cmd::Coverage(args) => coverage::run(&args),
        Cmd::Release(args) => release::run(&args),
        Cmd::Lint(args) => lint::run(&args),
        Cmd::Fixtures { cmd } => fixtures::run(&cmd),
    }
}

#[cfg(test)]
mod tests {
    use clap::CommandFactory as _;

    #[test]
    fn cli_is_well_formed() {
        super::Cli::command().debug_assert();
    }
}
