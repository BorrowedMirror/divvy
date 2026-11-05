//! `cargo xtask lint`: every static check CI runs, in one command.
use xshell::{Shell, cmd};

use crate::commit_config::root;

#[derive(Debug, clap::Args)]
pub struct LintArgs {
    /// Skip the web (eslint + tsc) checks.
    #[arg(long)]
    pub no_web: bool,
    /// Skip cargo-deny and cargo-machete (e.g. when they are not installed).
    #[arg(long)]
    pub no_tools: bool,
}

/// One check: a display name and the command line (`program args...`), run from `dir`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Step {
    pub name: &'static str,
    pub dir: &'static str,
    pub argv: &'static [&'static str],
}

pub fn steps(args: &LintArgs) -> Vec<Step> {
    let mut steps = vec![
        Step { name: "rustfmt", dir: ".", argv: &["cargo", "fmt", "--all", "--", "--check"] },
        Step {
            name: "clippy",
            dir: ".",
            argv: &["cargo", "clippy", "--workspace", "--all-targets", "--locked", "--", "-D", "warnings"],
        },
    ];
    if !args.no_tools {
        steps.push(Step { name: "cargo-deny", dir: ".", argv: &["cargo", "deny", "--locked", "check"] });
        steps.push(Step { name: "cargo-machete", dir: ".", argv: &["cargo", "machete"] });
    }
    if !args.no_web {
        steps.push(Step { name: "eslint", dir: "web", argv: &["pnpm", "lint"] });
        steps.push(Step { name: "tsc", dir: "web", argv: &["pnpm", "typecheck"] });
    }
    steps
}

pub fn run(args: &LintArgs) -> anyhow::Result<()> {
    let sh = Shell::new()?;
    let root = root();
    let mut failed = Vec::new();
    for step in steps(args) {
        sh.change_dir(root.join(step.dir));
        let (program, rest) = step.argv.split_first().expect("non-empty argv");
        println!("==> {}", step.name);
        if cmd!(sh, "{program} {rest...}").run().is_err() {
            failed.push(step.name);
        }
    }
    if failed.is_empty() {
        println!("lint: all checks passed");
        Ok(())
    } else {
        anyhow::bail!("lint failed: {}", failed.join(", "))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn names(args: &LintArgs) -> Vec<&'static str> {
        steps(args).iter().map(|s| s.name).collect()
    }

    #[test]
    fn full_lint_runs_everything() {
        assert_eq!(
            names(&LintArgs { no_web: false, no_tools: false }),
            ["rustfmt", "clippy", "cargo-deny", "cargo-machete", "eslint", "tsc"]
        );
    }

    #[test]
    fn flags_skip_groups() {
        assert_eq!(names(&LintArgs { no_web: true, no_tools: true }), ["rustfmt", "clippy"]);
        let clippy = &steps(&LintArgs { no_web: true, no_tools: true })[1];
        assert!(clippy.argv.ends_with(&["-D", "warnings"]));
    }
}
