import { render, screen } from "@testing-library/react";
import { App } from "./App";
import { routeFromHash } from "./route";

test("renders settings by default", () => {
  render(<App />);
  expect(screen.getByRole("main")).toHaveAttribute("data-route", "settings");
});

test("routeFromHash", () => {
  expect(routeFromHash("#/overlay")).toBe("overlay");
  expect(routeFromHash("#/diagnostics")).toBe("diagnostics");
  expect(routeFromHash("")).toBe("settings");
});
