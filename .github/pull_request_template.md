## What and why

<!-- What does this change and why is it needed? Link issues: Fixes #123 -->

## How was it tested?

<!-- Unit / integration / e2e tests added or updated; manual testing on which OS. -->

## Checklist

- [ ] PR title and commits follow [the commit conventions](../blob/master/docs/contributing/commit-message.md) (`<type>(<scope>): <summary>`)
- [ ] `cargo xtask lint` passes
- [ ] Tests added or updated; `cargo xtask coverage --check` passes
- [ ] `cargo xtask gen` run if IPC types or `.commit-config.toml` changed (`web/src/bindings.ts` is committed)
- [ ] User-visible change? Docs updated
- [ ] Wrong-price fix? The item text was added to `crates/testkit/corpus/`
