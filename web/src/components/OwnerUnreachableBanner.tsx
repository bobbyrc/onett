import { Link } from 'react-router-dom';
import { useCan } from '../auth';
import { ownerWarning } from '../lib/ownerUnreachable';
import { useStats } from '../queries';
import { Callout } from './Callout';

/** Shown on the Overview while only the log notifier can reach the owner, so nothing onett needs would be heard. */
export function OwnerUnreachableBanner() {
  const warning = ownerWarning(useStats().data);
  const owner = useCan('administer');
  if (!warning) return null;
  return (
    <Callout tone="danger" title="Nobody will be told">
      {warning.message}
      {' '}
      {owner ? (
        <>
          <Link to="/config?mode=setup">Set up a channel or notifier</Link> (Telegram, Discord or ntfy).
        </>
      ) : (
        'An owner can set up a channel or notifier (Telegram, Discord or ntfy) in Config.'
      )}
      {warning.goals.map((g) => (
        <p key={g.id}>
          Goal <Link to={`/goals/${g.id}`}>{g.title}</Link> {g.text}
        </p>
      ))}
    </Callout>
  );
}
