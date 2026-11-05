//! Repo tooling: `cargo xtask <cmd>`.
use clap::{Parser, Subcommand};

mod commit_config;

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
    /// Run coverage with per-crate gates (workstream I).
    Coverage,
    /// Generate derived files: .commitlintrc.json, docs/contributing/commit-message.md, bindings.
    Gen,
    /// Release helpers (workstream I).
    Release,
    /// fmt + clippy + deny + machete + web lint (workstream I).
    Lint,
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

fn main() -> anyhow::Result<()> {
    match Cli::parse().cmd {
        Cmd::Gen => commit_config::generate(),
        Cmd::Data { .. } => anyhow::bail!("not implemented yet: workstream C"),
        Cmd::Coverage | Cmd::Release | Cmd::Lint => anyhow::bail!("not implemented yet: workstream I"),
    }
}
