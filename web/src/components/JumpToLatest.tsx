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
      <span aria-hidden="true">↓</span> Jump to latest
      {unread > 0 ? <span className="jump-count">{unread} new</span> : null}
    </button>
  );
}
