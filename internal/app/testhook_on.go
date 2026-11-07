//go:build testhook

package app

// testHookBuild is true in builds with the test hook (`-tags testhook`): they honour the
// RKL_*_BASE_URL endpoint overrides and can start the hook on RKL_TEST_HOOK.
const testHookBuild = true
