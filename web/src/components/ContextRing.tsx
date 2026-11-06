/** A small ring showing how full the model's context window is; hidden when the fraction is unknown. */
export function ContextRing({ fraction }: { fraction: number | null }) {
  // No data (or a number that is not a usable fraction) draws nothing: an empty ring would read as a warning.
  if (fraction == null || !Number.isFinite(fraction) || fraction <= 0) return null;
  const pct = Math.round(fraction * 100);
  const r = 7;
  const c = 2 * Math.PI * r;
  return (
    <span
      className="context-ring"
      role="img"
      aria-label={`Context ${pct}% full`}
      title={`Context ${pct}% full`}
      data-level={fraction >= 0.9 ? 'high' : fraction >= 0.7 ? 'warn' : 'ok'}
    >
      <svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true" focusable="false">
        <circle cx="10" cy="10" r={r} fill="none" stroke="currentColor" strokeOpacity="0.25" strokeWidth="2.5" />
        <circle
          className="context-ring-arc"
          cx="10"
          cy="10"
          r={r}
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={`${c * fraction} ${c}`}
          transform="rotate(-90 10 10)"
        />
      </svg>
    </span>
  );
}
