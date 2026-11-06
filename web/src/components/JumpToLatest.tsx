// The "Jump to latest" pill shown above the composer while the reader is away from the end.
interface Props {
  unread: number;
  onClick: () => void;
}

export function JumpToLatest({ unread, onClick }: Props) {
  return (
    <button
      type="button"
      className="jump-latest"
      onClick={onClick}
      // Keep focus where it is (usually the composer) on a pointer press; keyboard activation still works.
      onMouseDown={(e) => e.preventDefault()}
    >
      <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
        <path d="M8 3v10M3.5 8.5 8 13l4.5-4.5" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      Jump to latest
      {unread > 0 ? <span className="jump-count">{unread} new</span> : null}
    </button>
  );
}
