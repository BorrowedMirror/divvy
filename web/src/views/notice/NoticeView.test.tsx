import { screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { emitEvent, flush, renderWithProviders, setupIpc } from "@test/ipc";
import { NoticeView } from "@views/notice/NoticeView";

describe("NoticeView", () => {
  test("says RKL is ready", async () => {
    setupIpc();
    renderWithProviders(<NoticeView />);
    const status = await screen.findByRole("status");
    expect(status).toHaveTextContent("Rust King Ledger");
    expect(status).toHaveTextContent("Ready and running in the background");
  });

  test("replays the fade for each ready-notice over the given duration", async () => {
    setupIpc();
    renderWithProviders(<NoticeView />);
    await flush();
    const first = screen.getByTestId("ready-notice");
    await emitEvent("ready-notice", { duration_ms: 2500 });
    const second = screen.getByTestId("ready-notice");
    expect(second).not.toBe(first);
    expect(second.style.getPropertyValue("--k-notice-ms")).toBe("2500ms");
  });
});
