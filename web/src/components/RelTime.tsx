import { formatDateTime, formatRelative, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';

/**
 * A relative time ("3m ago") with the full timestamp on hover. `spoken` also puts the full time
 * in the text a screen reader reads, for a place where the hover title is the only other copy.
 */
export function RelTime({ value, className, spoken }: { value: string | null | undefined; className?: string; spoken?: boolean }) {
  const now = useNow();
  const date = parseTime(value);
  if (!date) return <span className="muted">—</span>;
  const full = formatDateTime(date);
  return (
    <time className={className} dateTime={date.toISOString()} title={full}>
      {formatRelative(date, now)}
      {spoken ? <span className="sr-only">{`, ${full}`}</span> : null}
    </time>
  );
}
