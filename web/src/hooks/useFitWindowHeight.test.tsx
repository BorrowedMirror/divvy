import { render } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { callsOf, setupIpc } from "../test/ipc";
import { useFitWindowHeight } from "./useFitWindowHeight";

let observed: (() => void) | null = null;
let height = 300;

beforeEach(() => {
  observed = null;
  height = 300;
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(cb: () => void) {
        observed = cb;
      }
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    cb(0);
    return 1;
  });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
    () => ({ height, width: 420, top: 0, left: 0, right: 420, bottom: height, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect,
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function Panel() {
  const ref = useFitWindowHeight<HTMLDivElement>();
  return <div ref={ref}>panel</div>;
}

test("reports the content height on mount and whenever it changes", async () => {
  const calls = setupIpc({ resize_overlay: () => null });
  render(<Panel />);
  await vi.waitFor(() => expect(callsOf(calls, "resize_overlay")).toHaveLength(1));
  expect(callsOf(calls, "resize_overlay")[0]?.args).toEqual({ width: 420, height: 300 });

  observed?.(); // same height: no new resize
  expect(callsOf(calls, "resize_overlay")).toHaveLength(1);

  height = 512.4;
  observed?.();
  await vi.waitFor(() => expect(callsOf(calls, "resize_overlay")).toHaveLength(2));
  expect(callsOf(calls, "resize_overlay")[1]?.args).toEqual({ width: 420, height: 513 });
});

test("does nothing without ResizeObserver", () => {
  vi.stubGlobal("ResizeObserver", undefined);
  const calls = setupIpc({ resize_overlay: () => null });
  render(<Panel />);
  expect(callsOf(calls, "resize_overlay")).toHaveLength(0);
});
