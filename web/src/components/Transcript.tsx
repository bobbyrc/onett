import { Fragment, useId, useState, type ReactNode } from 'react';
import { cx, formatClock } from '../lib/format';
import { formatDuration, parseTime } from '../lib/time';
import { clipLines, isInternalLine, type Entry } from '../lib/transcript';
import type { TranscriptView } from '../lib/transcriptView';
import { WorkedToggle } from './ActivityLine';
import { CopyButton } from './CopyButton';
import { Markdown } from './Markdown';
import { MessageAttachments } from './MessageAttachments';
import { RelTime } from './RelTime';
import { ToolLine, ToolRun } from './ToolRun';
import { groupToolRuns, turnWork, type ToolEntry } from './toolRuns';
import './Transcript.css';

const RESULT_PREVIEW_LINES = 3;
/** Who sent an owner-side message that the owner did not write: only the orchestrator, today. */
const authorLabel = (author: string) => (author === 'orchestrator' ? 'Orchestrator' : author);

const QUEUED_NOTE = 'Queued · sends when the agent finishes this run';

function clock(time: string): string {
  const t = parseTime(time);
  return t ? formatClock(t) : '';
}

/** The Conversation / Terminal switch. The caller stores the choice. */
export function ViewToggle({ view, onChange }: { view: TranscriptView; onChange(v: TranscriptView): void }) {
  const options: { value: TranscriptView; label: string }[] = [
    { value: 'conversation', label: 'Conversation' },
    { value: 'terminal', label: 'Terminal' },
  ];
  return (
    <div className="tr-seg" role="group" aria-label="Transcript view">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          className={cx(view === o.value && 'on')}
          aria-pressed={view === o.value}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export interface TranscriptProps {
  entries: Entry[];
  view: TranscriptView;
  /** Who the agent prose belongs to: "Orchestrator", or a task agent's role such as "Implementer". */
  agentLabel: string;
  /** Rendered after the last entry of each run, for example a chip for tasks that run created. */
  afterRun?: (runId: string) => ReactNode;
  /** Rendered in the note of a queued owner message, for Edit and Cancel. */
  queuedActions?: (messageId: string) => ReactNode;
  /** The run that is producing output now. Without it, a trailing call with no result counts as live. */
  liveRunId?: string;
  /** Tokens each run used (input + output). The log has no per-call counts, so the total shows on the run's last tool row. */
  runTokens?: Record<string, number>;
}

/** A run of agent entries, or an owner's turn, laid out as a conversation or as terminal lines. */
export function Transcript(props: TranscriptProps) {
  if (props.view === 'terminal') return <TerminalView {...props} />;
  return <ConversationView {...props} entries={props.entries.filter((e) => !isInternalLine(e))} />;
}

const isAgentKind = (e: Entry) => e.kind === 'text' || e.kind === 'tool';

/** The slot after an entry that ends a run's stretch of entries. */
function runEnd(entries: Entry[], i: number, afterRun: TranscriptProps['afterRun']): ReactNode {
  const runId = entries[i].runId;
  if (!afterRun || !runId || entries[i + 1]?.runId === runId) return null;
  return afterRun(runId);
}

function ConversationView({ entries, agentLabel, afterRun, queuedActions, liveRunId, runTokens }: TranscriptProps) {
  const blocks = groupToolRuns(entries);
  const turns = turnWork(entries, liveRunId);
  // Each run's last tool entry: only there does the run's token total show, once.
  const lastToolOf = new Map<string, string>();
  for (const e of entries) if (e.kind === 'tool' && e.runId) lastToolOf.set(e.runId, e.id);
  const tokensEndingAt = (e: ToolEntry) => (e.runId && lastToolOf.get(e.runId) === e.id ? runTokens?.[e.runId] : undefined);
  // Turns the reader has flipped. A finished turn starts collapsed; its toggle is keyed by its first work entry.
  const [flipped, setFlipped] = useState<Record<string, boolean>>({});
  // Index in `entries` of each block's last entry, so afterRun and the agent label see the same neighbours.
  let end = -1;
  return (
    <div className="tr-col">
      {blocks.map((b, bi) => {
        const start = end + 1;
        end += b.type === 'run' ? b.calls.length : 1;
        const first = b.type === 'run' ? b.calls[0] : b.entry;
        const turn = turns.find((t) => t.ids.has(first.id));
        const open = turn ? flipped[turn.key] === true : true;
        const toggle = turn && turn.key === first.id ? (
          <WorkedToggle
            duration={turn.ms >= 1000 ? formatDuration(turn.ms) : undefined}
            open={open}
            onToggle={() => setFlipped({ ...flipped, [turn.key]: !open })}
          />
        ) : null;
        if (turn && !open) {
          // Collapsed work: only the toggle (at the turn's first entry) and any "created task" slot remain.
          return (
            <Fragment key={b.type === 'run' ? b.id : b.entry.id}>
              {toggle}
              {runEnd(entries, end, afterRun)}
            </Fragment>
          );
        }
        const labelled = start === 0 || !(isAgentKind(first) && isAgentKind(entries[start - 1]));
        const until = entries[end + 1]?.time;
        return (
          <Fragment key={b.type === 'run' ? b.id : b.entry.id}>
            {toggle}
            {b.type === 'run' ? (
              <div className="tr-ai tr-ai-tool">
                {labelled ? <div className="tr-lbl">{agentLabel}</div> : null}
                <ToolRun
                  calls={b.calls}
                  until={until}
                  tokens={tokensEndingAt(b.calls[b.calls.length - 1])}
                  active={bi === blocks.length - 1 && (liveRunId === undefined || b.calls[b.calls.length - 1].runId === liveRunId)}
                />
              </div>
            ) : (
              <ConversationEntry
                entry={b.entry}
                agentLabel={agentLabel}
                queuedActions={queuedActions}
                labelled={isAgentKind(b.entry) && labelled}
                live={bi === blocks.length - 1 && (liveRunId === undefined || b.entry.runId === liveRunId)}
                until={until}
                tokens={b.entry.kind === 'tool' ? tokensEndingAt(b.entry) : undefined}
              />
            )}
            {runEnd(entries, end, afterRun)}
          </Fragment>
        );
      })}
    </div>
  );
}

function ConversationEntry({
  entry: e,
  labelled,
  agentLabel,
  queuedActions,
  live = false,
  until,
  tokens,
}: {
  entry: Entry;
  live?: boolean;
  until?: string;
  tokens?: number;
  labelled: boolean;
  agentLabel: string;
  queuedActions: TranscriptProps['queuedActions'];
}) {
  switch (e.kind) {
    case 'owner':
      return (
        <div className="tr-row tr-row-you">
          <div className={cx('tr-you', e.queued && 'queued')}>
            {e.queued ? (
              <span className="tr-queued-note">
                {QUEUED_NOTE}
                {e.messageId && queuedActions ? queuedActions(e.messageId) : null}
              </span>
            ) : null}
            {e.author ? <span className="tr-queued-note">{authorLabel(e.author)}</span> : null}
            {e.text ? <FoldedText text={e.text} /> : null}
            {e.attachments?.length ? <MessageAttachments attachments={e.attachments} /> : null}
          </div>
          <MessageActions time={e.time} text={e.text} />
        </div>
      );
    case 'prompt':
      return <PromptSheet label={e.label} text={e.text} time={e.time} />;
    case 'text':
      return (
        <div className="tr-row tr-row-ai">
          <div className="tr-ai">
            {labelled ? <div className="tr-lbl">{agentLabel}</div> : null}
            <Markdown text={e.text} />
          </div>
          <MessageActions time={e.time} text={e.text} />
        </div>
      );
    case 'tool':
      return (
        <div className="tr-ai tr-ai-tool">
          {labelled ? <div className="tr-lbl">{agentLabel}</div> : null}
          <ToolLine entry={e} live={live} until={until} tokens={tokens} />
        </div>
      );
    case 'denial':
      return (
        <div className="tr-note tr-denial">
          <span className="tr-denied">Denied</span>
          <pre>{e.text}</pre>
        </div>
      );
    case 'error':
      return (
        <div className="tr-note tr-error">
          <pre>{e.text}</pre>
        </div>
      );
    case 'system':
      return (
        <div className="tr-note tr-muted">
          <pre>{e.text}</pre>
        </div>
      );
    case 'stderr':
      return (
        <div className="tr-note tr-muted tr-stderr">
          <pre>{e.text}</pre>
        </div>
      );
    case 'divider':
      return (
        <div className="tr-divider" role="separator">
          <span>{e.text}</span>
        </div>
      );
  }
}

/** A message longer than this many lines or characters folds. */
const FOLD_LINES = 12;
const FOLD_CHARS = 1200;

/** Whether `text` is long enough to fold: more than about 12 lines or 1200 characters. */
export const needsFold = (text: string) => text.length > FOLD_CHARS || text.split('\n').length > FOLD_LINES;

/**
 * An owner's message as pre-wrapped text. A long one shows about eight lines under a fade with a
 * Show more / Show less button; the copy button elsewhere still copies all of it.
 */
function FoldedText({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const foldable = needsFold(text);
  return (
    <>
      <span id={id} className={cx('tr-body', foldable && !open && 'folded')}>
        {text}
      </span>
      {foldable ? (
        <button type="button" className="tr-fold" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
          {open ? 'Show less' : 'Show more'}
        </button>
      ) : null}
    </>
  );
}

/** A task prompt can run to dozens of lines, so the conversation view folds it like any long message. */
function PromptSheet({ label, text, time }: { label: string; text: string; time: string }) {
  return (
    <div className="tr-row tr-row-you">
      <div className="tr-you">
        <div className="tr-lbl">{label}</div>
        <FoldedText text={text} />
      </div>
      <MessageActions time={time} text={text} />
    </div>
  );
}

/** The small row under a message: when it was sent, and a copy button. Hover or focus reveals it on a mouse; touch always shows it. */
function MessageActions({ time, text }: { time: string; text?: string }) {
  const hasTime = parseTime(time) !== null;
  if (!hasTime && !text) return null;
  return (
    <div className="tr-actions">
      {hasTime ? <RelTime value={time} className="tr-time" spoken /> : null}
      {text ? <CopyButton text={text} className="tr-copy" /> : null}
    </div>
  );
}

function TerminalView({ entries, afterRun, queuedActions }: TranscriptProps) {
  return (
    <div className="tr-term">
      {entries.map((e, i) => (
        <Fragment key={e.id}>
          <TerminalEntry entry={e} queuedActions={queuedActions} />
          {runEnd(entries, i, afterRun)}
        </Fragment>
      ))}
    </div>
  );
}

function TermLine({ time, marker, className, children }: { time?: string; marker: string; className: string; children: React.ReactNode }) {
  return (
    <div className={cx('tr-ln', className)}>
      <span className="tr-ts">{time ? clock(time) : ''}</span>
      <span className="tr-mk" aria-hidden="true">
        {marker}
      </span>
      <div className="tr-tx">{children}</div>
    </div>
  );
}

function TerminalEntry({ entry: e, queuedActions }: { entry: Entry; queuedActions: TranscriptProps['queuedActions'] }) {
  switch (e.kind) {
    case 'owner':
      return (
        <TermLine time={e.time} marker="›" className="k-user">
          {e.queued ? (
            <div className="tr-queued-note">
              {QUEUED_NOTE}
              {e.messageId && queuedActions ? queuedActions(e.messageId) : null}
            </div>
          ) : null}
          {e.author ? <div className="tr-queued-note">{authorLabel(e.author)}</div> : null}
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'prompt':
      return (
        <TermLine time={e.time} marker="›" className="k-user">
          <div className="tr-queued-note">{e.label}</div>
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'text':
      return (
        <TermLine time={e.time} marker="●" className="k-text">
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'tool':
      return <TerminalTool entry={e} />;
    case 'denial':
      return (
        <TermLine time={e.time} marker="●" className="k-denial">
          <span className="tr-denied">Denied</span> <pre>{e.text}</pre>
        </TermLine>
      );
    case 'error':
      return (
        <TermLine time={e.time} marker="●" className="k-error">
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'system':
    case 'stderr':
      return (
        <TermLine time={e.time} marker="●" className="k-res">
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'divider':
      return (
        <div className="tr-divider" role="separator">
          <span>{e.text}</span>
        </div>
      );
  }
}

function TerminalTool({ entry }: { entry: ToolEntry }) {
  const [all, setAll] = useState(false);
  const { tool } = entry;
  const { lines: shown, clipped } =
    entry.result === undefined ? { lines: [], clipped: false } : clipLines(entry.result, RESULT_PREVIEW_LINES, all);
  const diff = tool.added !== undefined || tool.removed !== undefined ? `  +${tool.added ?? 0} −${tool.removed ?? 0}` : '';
  return (
    <>
      <TermLine time={entry.time} marker="●" className="k-tool">
        <pre>
          <span className="tr-name">{tool.name}</span>
          {tool.arg ? `  ${tool.arg}` : ''}
          {diff}
        </pre>
      </TermLine>
      {entry.result !== undefined ? (
        <TermLine marker="⎿" className={cx('k-res', entry.resultError && 'k-error')}>
          <pre>{shown.join('\n')}</pre>
          {clipped ? (
            <button type="button" className="link-button" onClick={() => setAll(!all)}>
              {all ? 'show less' : 'show more'}
            </button>
          ) : null}
        </TermLine>
      ) : null}
    </>
  );
}
