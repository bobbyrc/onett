// The "Send test" button the visual editor shows on each notify[i] and channels[i] entry. It is
// injected by path so ConfigSection only has to render it. It is disabled while the entry differs
// from the saved file, because the daemon tests what is saved.
import { getAt, type Path } from '../../lib/configDoc';
import type { EditContext } from './ConfigSection';
import { SendTestButton } from './SendTestButton';

export function EntryTest({ path, ctx }: { path: Path; ctx: EditContext }) {
  const [list, index] = path;
  if (path.length !== 2 || (list !== 'notify' && list !== 'channels')) return null;
  // No saved copy known (a caller that does not pass one): nothing to compare, so no button.
  if (!('saved' in ctx)) return null;
  const unsaved = JSON.stringify(getAt(ctx.draft, path)) !== JSON.stringify(getAt(ctx.saved, path));
  return <SendTestButton kind={list} index={Number(index)} label="Send test" disabledReason={unsaved ? 'Save first' : undefined} />;
}
