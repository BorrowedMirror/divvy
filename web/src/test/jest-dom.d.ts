// pnpm links @testing-library/jest-dom to the e2e package's `vitest` peer variant, so its own
// `declare module "vitest"` augments a different copy than web uses. Re-declare the matchers
// against web's `vitest` so `expect(el).toBeInTheDocument()` type-checks.
import "vitest";
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

declare module "vitest" {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-empty-object-type
  interface Assertion<T = any> extends TestingLibraryMatchers<any, T> {}
  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-empty-object-type
  interface AsymmetricMatchersContaining extends TestingLibraryMatchers<any, any> {}
}
