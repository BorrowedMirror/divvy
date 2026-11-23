//go:build testhook

package app

// testHookBuild is true in builds with the test hook (`-tags testhook`): they honour the
// DIVVY_*_BASE_URL endpoint overrides and can start the hook on DIVVY_TEST_HOOK.
const testHookBuild = true
