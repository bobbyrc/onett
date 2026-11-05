// The "Jump to latest" pill shown above the composer while the reader is away from the end.
interface Props {
  unread: number;
  onClick: () => void;
}

export function JumpToLatest({ unread, onClick }: Props) {
  return (
    <button type="button" className="jump-latest" onClick={onClick}>
      <span aria-hidden="true">↓</span> Jump to latest
      {unread > 0 ? <span className="jump-count">{unread} new</span> : null}
    </button>
  );
}
