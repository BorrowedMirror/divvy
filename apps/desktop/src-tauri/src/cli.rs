//! Minimal CLI: `kalandra [--trigger [--pinned]]`. `--trigger` asks the running instance to
//! price-check (e.g. a Wayland compositor key binding when global shortcuts are unavailable);
//! the argv is forwarded by the single-instance plugin.

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Cli {
    pub trigger: bool,
    pub pinned: bool,
}

pub fn parse(args: impl IntoIterator<Item = String>) -> Cli {
    let mut cli = Cli::default();
    for a in args {
        match a.as_str() {
            "--trigger" => cli.trigger = true,
            "--pinned" => cli.pinned = true,
            _ => {}
        }
    }
    cli
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_flags() {
        assert_eq!(parse(["kalandra".into()]), Cli::default());
        assert_eq!(parse(["--trigger".into(), "--pinned".into()]), Cli { trigger: true, pinned: true });
        // Forwarded argv: the callback skips argv[0].
        let argv = vec!["/usr/bin/kalandra".to_owned(), "--trigger".to_owned()];
        assert_eq!(parse(argv.into_iter().skip(1)), Cli { trigger: true, pinned: false });
    }
}
