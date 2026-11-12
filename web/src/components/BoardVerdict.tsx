import type { Verdict } from '../lib/verdict';
import { LineWindow } from './AndonBoard';

/** The sentence on the board: what to do first, with the line window beside it. */
export function BoardVerdict({ verdict }: { verdict: Verdict }) {
  return (
    <section className="ov-verdict" aria-label="What to do first">
      <div className="ov-verdict-main">
        <h2 className="ov-verdict-head">
          <span className={`lamp lamp-${verdict.lamp}`} aria-hidden="true" />
          {verdict.head}
        </h2>
        <p className="ov-verdict-text">
          {verdict.lead ? <b>{verdict.lead}</b> : null}
          {verdict.lead && verdict.rest ? ' ' : null}
          {verdict.rest}
        </p>
      </div>
      <LineWindow />
    </section>
  );
}
