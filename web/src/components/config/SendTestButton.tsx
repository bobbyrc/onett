// A "Send test" button for a saved notifier or channel, with the result shown beside it. Used by
// the guided setup and by each notify / channels entry in the visual editor. It sends through
// the saved config, so callers disable it while the entry has unsaved edits.
import { useSendTest } from '../../queries';
import { errorMessage } from '../../lib/format';
import { Button } from '../Button';

export function SendTestButton({
  kind,
  index,
  label,
  disabledReason,
}: {
  kind: 'notify' | 'channels';
  index: number;
  label?: string;
  /** When set the button is disabled and this is why ("Save first"). */
  disabledReason?: string;
}) {
  const test = useSendTest();
  const text = label ?? (kind === 'notify' ? 'Send test notification' : 'Send test message');
  const result = test.data;
  return (
    <span className="send-test">
      <Button
        disabled={test.isPending || Boolean(disabledReason)}
        title={disabledReason}
        onClick={() => test.mutate({ kind, index })}
      >
        {test.isPending ? 'Sending…' : text}
      </Button>
      {disabledReason ? <span className="muted small"> {disabledReason}</span> : null}
      <span role="status" className="send-test-result">
        {result?.ok ? <span className="small"> Sent. Check that it arrived.</span> : null}
        {result && !result.ok ? <span className="error-line small"> Failed: {result.error}</span> : null}
        {test.error ? <span className="error-line small"> Failed: {errorMessage(test.error)}</span> : null}
      </span>
    </span>
  );
}
