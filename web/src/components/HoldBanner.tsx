import { useStats } from '../queries';
import { useNow } from '../lib/clock';
import { formatDateTime, parseTime } from '../lib/time';
import { Callout } from './Callout';

/**
 * A note for each harness held for a rate limit: no new run starts on it until the hold lifts, so
 * queued work looks stuck without it. Renders nothing when no hold is active.
 */
export function HoldBanner() {
  const stats = useStats();
  const now = useNow();
  const active = (stats.data?.holds ?? []).filter((h) => {
    const until = parseTime(h.until);
    return until !== null && until.getTime() > now;
  });
  if (active.length === 0) return null;

  return (
    <>
      {active.map((hold) => {
        const until = parseTime(hold.until)!;
        return (
          <Callout key={hold.harness} tone="info" title={`${hold.harness} is rate limited`}>
            New runs wait until{' '}
            <time dateTime={until.toISOString()} title={formatDateTime(until)}>
              {until.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </time>
            .
          </Callout>
        );
      })}
    </>
  );
}
