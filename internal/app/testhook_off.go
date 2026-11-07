//go:build !testhook

package app

// testHookBuild is false in release builds: no endpoint overrides, no test hook.
const testHookBuild = false
