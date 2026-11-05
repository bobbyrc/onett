// The pieces of "Restart now": where the daemon will be listening afterwards, and waiting for
// it to come back. The network and the clock are passed in so tests need neither.

const WILDCARD_HOSTS = new Set(["", "0.0.0.0", "::", "[::]"]);
const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "::1", "[::1]"]);

export interface Where {
  protocol: string;
  hostname: string;
  port: string;
}

/**
 * The origin the UI will be served from after a restart with server.addr = addr, or null when it
 * stays where the browser already is (no addr, or the same port). A wildcard bind address is not a
 * name a browser can use, so the current host name stays; so does a non-loopback host name when
 * the new addr is loopback (the browser reached us through a proxy or a LAN name).
 */
export function restartOrigin(
  addr: string | undefined,
  here: Where,
): string | null {
  if (!addr) return null;
  const cut = addr.lastIndexOf(":");
  const port = cut >= 0 ? addr.slice(cut + 1) : "";
  const host = cut >= 0 ? addr.slice(0, cut) : addr;
  if (!/^\d+$/.test(port)) return null;
  const keepHost =
    WILDCARD_HOSTS.has(host) ||
    (LOOPBACK_HOSTS.has(host) && !LOOPBACK_HOSTS.has(here.hostname));
  const hostname = keepHost ? here.hostname : host;
  // The same port is the same daemon, whatever name the bind address uses for this machine.
  if (port === here.port) return null;
  const shownHost =
    hostname.includes(":") && !hostname.startsWith("[")
      ? `[${hostname}]`
      : hostname;
  return `${here.protocol}//${shownHost}:${port}`;
}

export interface WaitOptions {
  /** The origin to poll; '' is the page's own origin. */
  origin: string;
  /** started_at of the daemon being restarted; a same-origin answer with this value is the old one. */
  oldStartedAt?: string;
  timeoutMs?: number;
  intervalMs?: number;
  fetchFn?: typeof fetch;
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
}

/**
 * Polls /api/v1/health until the restarted daemon answers; true when it did within the timeout.
 * On the page's own origin the answer must be a different process (a new started_at). On another
 * origin the browser hides the answer (no CORS), so any reply counts.
 */
export async function waitForDaemon(o: WaitOptions): Promise<boolean> {
  const fetchFn = o.fetchFn ?? fetch;
  const sleep =
    o.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  const now = o.now ?? Date.now;
  const deadline = now() + (o.timeoutMs ?? 60_000);
  const sameOrigin = o.origin === "";
  while (now() < deadline) {
    await sleep(o.intervalMs ?? 1_000);
    try {
      if (sameOrigin) {
        const res = await fetchFn("/api/v1/health", { cache: "no-store" });
        if (res.ok) {
          const body = (await res.json()) as {
            ok?: boolean;
            started_at?: string;
          };
          if (
            body.ok &&
            (!o.oldStartedAt || body.started_at !== o.oldStartedAt)
          )
            return true;
        }
      } else {
        await fetchFn(`${o.origin}/api/v1/health`, {
          cache: "no-store",
          mode: "no-cors",
        });
        return true;
      }
    } catch {
      // Not listening yet.
    }
  }
  return false;
}
