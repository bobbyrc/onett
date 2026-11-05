import { describe, expect, it, vi } from "vitest";
import { restartOrigin, waitForDaemon } from "./restart";

const here = { protocol: "http:", hostname: "localhost", port: "7777" };

describe("restartOrigin", () => {
  it("stays put when the port is unchanged or addr is absent", () => {
    expect(restartOrigin(undefined, here)).toBeNull();
    expect(restartOrigin("127.0.0.1:7777", here)).toBeNull();
  });
  it("moves to the new port", () => {
    expect(restartOrigin("127.0.0.1:9000", here)).toBe("http://127.0.0.1:9000");
  });
  it("keeps the browser host for a wildcard bind", () => {
    expect(
      restartOrigin("0.0.0.0:9000", { ...here, hostname: "onett.lan" }),
    ).toBe("http://onett.lan:9000");
    expect(restartOrigin(":9000", here)).toBe("http://localhost:9000");
  });
  it("keeps a remote host name when the new addr is loopback", () => {
    expect(
      restartOrigin("127.0.0.1:9000", { ...here, hostname: "onett.lan" }),
    ).toBe("http://onett.lan:9000");
  });
  it("ignores an addr without a numeric port", () => {
    expect(restartOrigin("nonsense", here)).toBeNull();
  });
});

function clock() {
  let t = 0;
  return { now: () => t, sleep: async (ms: number) => void (t += ms) };
}

describe("waitForDaemon", () => {
  it("ignores the old process and succeeds on a new started_at", async () => {
    const answers = ["old", "old", "new"];
    const fetchFn = vi.fn(async () => {
      const started_at = answers.shift();
      return new Response(JSON.stringify({ ok: true, started_at }), {
        status: 200,
      });
    });
    const ok = await waitForDaemon({
      origin: "",
      oldStartedAt: "old",
      fetchFn,
      ...clock(),
    });
    expect(ok).toBe(true);
    expect(fetchFn).toHaveBeenCalledTimes(3);
  });
  it("survives connection errors while the daemon is down", async () => {
    let calls = 0;
    const fetchFn = vi.fn(async () => {
      if (++calls < 3) throw new TypeError("fetch failed");
      return new Response(JSON.stringify({ ok: true, started_at: "new" }), {
        status: 200,
      });
    });
    expect(
      await waitForDaemon({
        origin: "",
        oldStartedAt: "old",
        fetchFn,
        ...clock(),
      }),
    ).toBe(true);
  });
  it("accepts any reply from another origin", async () => {
    const fetchFn = vi.fn(async () => new Response(null));
    expect(
      await waitForDaemon({ origin: "http://h:9", fetchFn, ...clock() }),
    ).toBe(true);
    expect(fetchFn).toHaveBeenCalledWith(
      "http://h:9/api/v1/health",
      expect.objectContaining({ mode: "no-cors" }),
    );
  });
  it("gives up after the timeout", async () => {
    const fetchFn = vi.fn(async () => {
      throw new TypeError("down");
    });
    expect(
      await waitForDaemon({
        origin: "",
        fetchFn,
        timeoutMs: 5_000,
        ...clock(),
      }),
    ).toBe(false);
  });
});
