// pnpm links @testing-library/jest-dom to the e2e package's `vitest` peer variant, so its own
// `declare module "vitest"` augments a different copy than web uses. Re-declare the matchers
// against web's `vitest` so `expect(el).toBeInTheDocument()` type-checks.
import "vitest";
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

declare module "vitest" {
  // biome-ignore lint/suspicious/noExplicitAny: mirrors vitest's own `Assertion<T = any>` declaration.
  interface Assertion<T = any> extends TestingLibraryMatchers<any, T> {}
  // biome-ignore lint/suspicious/noExplicitAny: matchers are untyped in vitest's asymmetric form too.
  interface AsymmetricMatchersContaining extends TestingLibraryMatchers<any, any> {}
}
