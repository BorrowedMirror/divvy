//! fake-game: opens a window titled "Path of Exile 2". On Ctrl+C / Ctrl+Alt+C it puts the
//! chosen corpus item (normal / advanced variant) on the clipboard. Workstream H.
use clap::Parser;

#[derive(Debug, Parser)]
struct Args {
    /// Corpus item text file to serve on Ctrl+C.
    #[arg(long)]
    item: Option<std::path::PathBuf>,
}

fn main() -> anyhow::Result<()> {
    let _args = Args::parse();
    todo!("workstream H")
}
