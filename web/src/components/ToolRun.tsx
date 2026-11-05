import { useState } from 'react';
import { cx } from '../lib/format';
import { changedFiles, isLiveRun, liveLabel, summarizeRun, type ToolEntry } from './toolRuns';
import './ToolRun.css';

const Diff = ({ added, removed }: { added: number; removed: number }) => (
  <span className="tl-diff">
    <span className="a">+{added}</span> <span className="d">−{removed}</span>
  </span>
);

/** One tool call: verb, main argument, `+n −m` for edits. Opens to its input and output. */
export function ToolLine({ entry }: { entry: ToolEntry }) {
  const [open, setOpen] = useState(false);
  const { tool } = entry;
  const hasDiff = tool.added !== undefined || tool.removed !== undefined;
  return (
    <div className="tl">
      <button type="button" className="tl-row" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="tl-chev" aria-hidden="true">
          {open ? '▾' : '▸'}
        </span>
        <span className="tl-name">{tool.name}</span>
        <span className="tl-arg">{tool.arg}</span>
        {entry.resultError ? <span className="tl-err">error</span> : null}
        {hasDiff ? <Diff added={tool.added ?? 0} removed={tool.removed ?? 0} /> : null}
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
export function ToolRun({ calls, active }: { calls: ToolEntry[]; active: boolean }) {
  const [open, setOpen] = useState(false);
  const live = active && isLiveRun(calls);
  const files = changedFiles(calls);
  const added = files.reduce((n, f) => n + f.added, 0);
  const removed = files.reduce((n, f) => n + f.removed, 0);
  const errors = calls.filter((c) => c.resultError).length;
  return (
    <div className="tl tl-run">
      <button type="button" className="tl-row tl-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="tl-chev" aria-hidden="true">
          {open ? '▾' : '▸'}
        </span>
        {live ? <span className="tl-lamp" aria-hidden="true" /> : null}
        {/* Polite, and only the text changes: a new call or the settled summary is announced, a repeat is not. */}
        <span className={cx('tl-summary', live && 'live')} aria-live="polite" aria-atomic="true">
          {live ? liveLabel(calls[calls.length - 1]) : summarizeRun(calls)}
        </span>
        {errors > 0 ? <span className="tl-err">{errors === 1 ? '1 error' : `${errors} errors`}</span> : null}
        {files.length > 0 ? (
          <span className="tl-total">
            <Diff added={added} removed={removed} />
            <span className="tl-files">
              {files.length} {files.length === 1 ? 'file' : 'files'}
            </span>
          </span>
        ) : null}
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
          {calls.map((c) => (
            <ToolLine key={c.id} entry={c} />
          ))}
        </div>
      ) : null}
    </div>
  );
}
