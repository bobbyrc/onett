// @vitest-environment jsdom
// The Restart now button, rendered with react-dom and jsdom and a mocked API: the confirmation
// names the settings, the restart is posted, and the page reloads once a new process answers.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ getHealth: vi.fn(), restartDaemon: vi.fn() }));
const wait = vi.hoisted(() => ({ waitForDaemon: vi.fn() }));
vi.mock("../../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../api")>()),
  ...api,
}));
vi.mock("../../lib/restart", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/restart")>()),
  ...wait,
}));

import { RestartNow } from "./RestartNow";

let root: Root | undefined;
let host: HTMLElement;

beforeAll(() => {
  (
    globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
  ).IS_REACT_ACT_ENVIRONMENT = true;
});

afterEach(() => {
  act(() => root?.unmount());
  root = undefined;
  host?.remove();
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

function mount(props: React.ComponentProps<typeof RestartNow>) {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(<RestartNow {...props} />));
}

async function press() {
  await act(async () => {
    host.querySelector("button")!.click();
  });
}

describe("RestartNow", () => {
  it("does nothing when the confirmation is declined, and names the settings in it", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    mount({
      settings: ["server.addr", "server.data_dir"],
      addr: "127.0.0.1:9000",
    });
    await press();
    expect(confirm).toHaveBeenCalledWith(
      expect.stringContaining("server.addr, server.data_dir"),
    );
    expect(api.restartDaemon).not.toHaveBeenCalled();
  });

  it("restarts, waits for a new process at the new address, then moves the browser there", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    api.getHealth.mockResolvedValue({
      ok: true,
      version: "v",
      started_at: "old",
    });
    api.restartDaemon.mockResolvedValue({ ok: true });
    wait.waitForDaemon.mockResolvedValue(true);
    const assign = vi.fn();
    vi.stubGlobal("location", {
      protocol: "http:",
      hostname: "localhost",
      port: "7777",
      pathname: "/config",
      search: "",
      assign,
    });
    mount({ settings: ["server.addr"], addr: "127.0.0.1:9000" });
    await press();
    expect(api.restartDaemon).toHaveBeenCalledTimes(1);
    expect(wait.waitForDaemon).toHaveBeenCalledWith(
      expect.objectContaining({
        origin: "http://127.0.0.1:9000",
        oldStartedAt: "old",
      }),
    );
    expect(assign).toHaveBeenCalledWith("http://127.0.0.1:9000/config");
    vi.unstubAllGlobals();
  });

  it("says so when the daemon does not come back", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    api.getHealth.mockResolvedValue({ ok: true, version: "v" });
    api.restartDaemon.mockResolvedValue({ ok: true });
    wait.waitForDaemon.mockResolvedValue(false);
    mount({ settings: ["server.data_dir"] });
    await press();
    expect(host.textContent).toContain("did not come back within a minute");
  });

  it("shows the error when the restart is refused", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    api.getHealth.mockResolvedValue({ ok: true, version: "v" });
    api.restartDaemon.mockRejectedValue(new Error("nope"));
    mount({ settings: ["server.addr"] });
    await press();
    expect(host.textContent).toContain("Could not restart: nope");
    expect(wait.waitForDaemon).not.toHaveBeenCalled();
  });
});
