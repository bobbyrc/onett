// The "Restart now" button in the restart-needed notice. It asks first, names the settings,
// restarts the daemon, then waits for it (at the new address if server.addr changed) and moves
// the browser there.
import { useState } from "react";
import * as api from "../../api";
import { errorMessage } from "../../lib/format";
import { restartOrigin, waitForDaemon } from "../../lib/restart";

export function RestartNow({
  settings,
  addr,
}: {
  settings: string[];
  addr?: string;
}) {
  const [phase, setPhase] = useState<"idle" | "restarting" | "timeout">("idle");
  const [error, setError] = useState<string | null>(null);

  const run = async () => {
    const list = settings.join(", ");
    if (
      !window.confirm(
        `Restart onett now to apply ${list}?\n\nRunning agents are stopped the way a normal shutdown stops them, and the UI is unavailable for a few seconds.`,
      )
    )
      return;
    setError(null);
    setPhase("restarting");
    try {
      const before = await api.getHealth().catch(() => null);
      await api.restartDaemon();
      const target = settings.includes("server.addr")
        ? restartOrigin(addr, window.location)
        : null;
      const back = await waitForDaemon({
        origin: target ?? "",
        oldStartedAt: before?.started_at,
      });
      if (!back) {
        setPhase("timeout");
        return;
      }
      if (target)
        window.location.assign(
          target + window.location.pathname + window.location.search,
        );
      else window.location.reload();
    } catch (err) {
      setPhase("idle");
      setError(errorMessage(err));
    }
  };

  return (
    <span className="restart-now">
      <button
        type="button"
        className="link-button"
        disabled={phase === "restarting"}
        onClick={() => void run()}
      >
        {phase === "restarting" ? "Restarting…" : "Restart now"}
      </button>
      <span role="status">
        {phase === "timeout" ? (
          <span className="error-line small">
            {" "}
            onett did not come back within a minute. Check the daemon log, or
            start it from a terminal with `onett serve`.
          </span>
        ) : null}
        {error ? (
          <span className="error-line small"> Could not restart: {error}</span>
        ) : null}
      </span>
    </span>
  );
}
