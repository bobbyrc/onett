import { useState } from 'react';
import { cx, formatTokens } from '../lib/format';
import { formatDuration, parseTime } from '../lib/time';
import { changedFiles, isLiveRun, liveLabel, summarizeRun, type ToolEntry } from './toolRuns';
import './ToolRun.css';

const Diff = ({ added, removed }: { added: number; removed: number }) => (
  <span className="tl-diff">
    <span className="a">+{added}</span> <span className="d">−{removed}</span>
  </span>
);

/** The state a tool lamp shows: blinking while live, then done or failed in onett's lamp colours. */
type LampState = 'live' | 'done' | 'failed' | 'off';

const lampLabel: Record<LampState, string> = { live: 'Running', done: 'Completed', failed: 'Failed', off: 'No result' };

/** The lamp sits before the text. The status words ride in the metadata, so the lamp never carries it alone. */
const Lamp = ({ state }: { state: LampState }) => <span className={cx('tl-lamp', `tl-lamp-${state}`)} aria-hidden="true" />;

const Chevron = ({ open }: { open: boolean }) => <span className={cx('tl-chev', open && 'open')} aria-hidden="true" />;

/** "1m 44s" between two entry times, or nothing when either is unknown or under a second. */
export function spanLabel(from: string, until?: string): string | undefined {
  const a = parseTime(from);
  const b = parseTime(until);
  if (!a || !b) return undefined;
  const ms = b.getTime() - a.getTime();
  return ms >= 1000 ? formatDuration(ms) : undefined;
}

/** How long a running call has gone, as last reported by the agent's progress heartbeats. */
const liveElapsed = (e: ToolEntry): string | undefined =>
  e.elapsedSeconds !== undefined && e.elapsedSeconds >= 1 ? formatDuration(e.elapsedSeconds * 1000) : undefined;

/** Right-aligned metadata: status · duration · tokens. Only what is known; the log has no per-call token count, so tokens are the whole run's total. */
function Meta({ status, danger, duration, tokens }: { status: string; danger?: boolean; duration?: string; tokens?: number }) {
  return (
    <span className="tl-meta">
      <span className={cx(danger && 'tl-meta-danger')}>{status}</span>
      {duration ? <span className="tl-meta-more"> · {duration}</span> : null}
      {tokens ? <span className="tl-meta-more"> · {formatTokens(tokens)} tokens</span> : null}
    </span>
  );
}

/** One tool call: verb, main argument, `+n −m` for edits. Opens to its input and output. */
export function ToolLine({ entry, live = false, until, tokens }: { entry: ToolEntry; live?: boolean; /** Time of whatever came next, to say how long the call took. */ until?: string; /** Token total of the run this call ends. */ tokens?: number }) {
  const [open, setOpen] = useState(false);
  const { tool } = entry;
  const hasDiff = tool.added !== undefined || tool.removed !== undefined;
  const lamp: LampState = entry.resultError ? 'failed' : entry.result !== undefined ? 'done' : live ? 'live' : 'off';
  return (
    <div className="tl">
      <button type="button" className="tl-row" aria-expanded={open} onClick={() => setOpen(!open)}>
        <Lamp state={lamp} />
        <span className="tl-text">
          <span className="tl-name">{tool.name}</span>
          {tool.arg ? <span className="tl-arg">{tool.arg}</span> : null}
        </span>
        <Chevron open={open} />
        {hasDiff ? <Diff added={tool.added ?? 0} removed={tool.removed ?? 0} /> : null}
        <Meta
          status={lampLabel[lamp].toLowerCase()}
          danger={lamp === 'failed'}
          duration={lamp === 'live' ? liveElapsed(entry) : spanLabel(entry.time, until)}
          tokens={lamp === 'live' ? undefined : tokens}
        />
      </button>
      {open ? (
        <div className="tl-body">
          {entry.input ? <pre tabIndex={0}>{entry.input}</pre> : null}
          {entry.result !== undefined ? (
            <pre tabIndex={0} className={cx(entry.resultError && 'tl-result-error')}>{entry.result}</pre>
          ) : (
            <pre className="tl-pending">No result yet.</pre>
          )}
        </div>
      ) : null}
    </div>
  );
}

/**
 * Two or more consecutive tool calls as one line. While the run is live the line names the latest
 * call; once it settles it reads as a sentence about what was done. Open, it lists the calls.
 */
export function ToolRun({ calls, active, until, tokens }: { calls: ToolEntry[]; active: boolean; /** Time of the entry after the run, to say how long it took. */ until?: string; /** Token total of the agent run this group ends. */ tokens?: number }) {
  const [open, setOpen] = useState(false);
  const live = active && isLiveRun(calls);
  const files = changedFiles(calls);
  const added = files.reduce((n, f) => n + f.added, 0);
  const removed = files.reduce((n, f) => n + f.removed, 0);
  const errors = calls.filter((c) => c.resultError).length;
  const lamp: LampState = live ? 'live' : errors > 0 ? 'failed' : 'done';
  return (
    <div className="tl tl-run">
      <button type="button" className="tl-row tl-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        <Lamp state={lamp} />
        {/* Polite, and only the text changes: a new call or the settled summary is announced, a repeat is not. */}
        <span className={cx('tl-summary', live && 'live')} aria-live="polite" aria-atomic="true">
          {live ? liveLabel(calls[calls.length - 1]) : summarizeRun(calls)}
        </span>
        <Chevron open={open} />
        {files.length > 0 ? (
          <span className="tl-total">
            <Diff added={added} removed={removed} />
            <span className="tl-files">
              {files.length} {files.length === 1 ? 'file' : 'files'}
            </span>
          </span>
        ) : null}
        <Meta
          status={live ? 'running' : errors > 0 ? (errors === 1 ? '1 error' : `${errors} errors`) : 'completed'}
          danger={!live && errors > 0}
          duration={live ? undefined : spanLabel(calls[0].time, until)}
          tokens={live ? undefined : tokens}
        />
      </button>
      {open ? (
        <div className="tl-calls">
          {files.length > 0 ? (
            <ul className="tl-changed" aria-label="Changed files">
              {files.map((f) => (
                <li key={f.path}>
                  <span className="tl-path">{f.path}</span>
                  <Diff added={f.added} removed={f.removed} />
                </li>
              ))}
            </ul>
          ) : null}
          {calls.map((c, i) => (
            <ToolLine key={c.id} entry={c} live={live && c === calls[calls.length - 1]} until={calls[i + 1]?.time ?? until} />
          ))}
        </div>
      ) : null}
    </div>
  );
}
