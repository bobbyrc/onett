import './Chat.css';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { ApiError, putConversationSettings } from '../api';
import { useCan } from '../auth';
import { cx, errorMessage } from '../lib/format';
import { agentLamp, composerState, taskLamp, type ChatLampState } from '../lib/chatState';
import { formatRelative, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';
import { orchestratorEntries, taskEntries } from '../lib/transcript';
import { readTranscriptView, writeTranscriptView, type TranscriptView } from '../lib/transcriptView';
import { useRunLogsMap } from '../runLogs';
import type { AgentConversation, ChatMessage, ConversationSettings, LogLine, Run, Task } from '../types';
import {
  useAgentConversations,
  useCancelMessage,
  useCloseTaskConversation,
  useConversation,
  useConfig,
  useConversations,
  useCreateConversation,
  useEditMessage,
  useFlushQueued,
  useReopenTaskConversation,
  useRun,
  useRunsMap,
  useSendMessage,
  useSendTaskMessage,
  useStopRun,
  useTaskConversation,
  useTasks,
} from '../queries';
import { ActivityLine, attentionFor, type Attention } from '../components/ActivityLine';
import { Button } from '../components/Button';
import { ChatSkeleton, ChatState } from '../components/ChatState';
import { ErrorLine } from '../components/ErrorLine';
import { Modal } from '../components/Modal';
import { PageHeader } from '../components/PageHeader';
import { Transcript, ViewToggle } from '../components/Transcript';
import { MessageRail } from '../components/MessageRail';
import { Composer, ComposerArea } from '../components/Composer';
import { ComposerSettings, DraftComposerSettings } from '../components/ComposerSettings';
import { ContextRing } from '../components/ContextRing';
import { contextFraction } from '../lib/composerSettings';
import { useAttachmentUploads, useLocalAttachments } from '../components/ComposerAttachments';
import { DEFAULT_MAX_PER_MESSAGE } from '../lib/attachments';
import { useStickToBottom } from '../lib/useStickToBottom';
import { useDraft } from '../lib/useDraft';
import { agentLabel } from '../lib/agentLabel';

const EXAMPLES = ["What's running right now?", 'Fix the flaky test in …', 'Triage open issues in …'];

/** Input + output tokens per run id, for the tool-row metadata. */
const tokensByRun = (runs: Run[]) => Object.fromEntries(runs.map((r) => [r.id, r.input_tokens + r.output_tokens]));

const capitalise = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);

/** The Conversation / Terminal choice, remembered per browser. */
function useTranscriptView(): [TranscriptView, (v: TranscriptView) => void] {
  const [view, setView] = useState<TranscriptView>(readTranscriptView);
  return [
    view,
    (next) => {
      setView(next);
      writeTranscriptView(next);
    },
  ];
}

export function Chat() {
  const { id, taskId } = useParams();
  const navigate = useNavigate();
  const conversations = useConversations();
  const agents = useAgentConversations();
  const create = useCreateConversation();
  const canOperate = useCan('operate');
  const [view, setView] = useTranscriptView();
  const hasConversations = (conversations.data?.length ?? 0) > 0 || (agents.data?.length ?? 0) > 0;
  const open = id !== undefined || taskId !== undefined;
  // The list column appears when a conversation is open or there is one to list.
  const showList = open || hasConversations;

  // A new chat started from the composer: create it, then open it with the message to send.
  // The chosen settings are stored before the message goes, so its first turn runs with them. A
  // failed start keeps the conversation it created, so a retry does not leave a second empty one.
  const created = useRef<string | null>(null);
  const [startError, setStartError] = useState<string | null>(null);
  const startFromComposer = async (start: StartRequest) => {
    setStartError(null);
    try {
      created.current ??= (await create.mutateAsync('New chat')).id;
      const hasSettings = Object.values(start.settings).some(Boolean);
      // The picker already asked the owner to confirm a dangerous mode.
      if (hasSettings) await putConversationSettings(created.current, start.settings, true);
      const conversationId = created.current;
      created.current = null;
      navigate(`/chat/${conversationId}`, { state: { message: start.text, files: start.files } });
    } catch (err) {
      setStartError(errorMessage(err));
      throw err;
    }
  };

  let noneOpen = null;
  if (canOperate) {
    noneOpen = <StartChat starting={create.isPending} onStart={startFromComposer} />;
  } else if (hasConversations) {
    noneOpen = (
      <ChatState icon="select" title="Pick a conversation">
        Choose one from the list to read it.
      </ChatState>
    );
  } else if (conversations.isSuccess) {
    noneOpen = (
      <ChatState icon="chat" title="No conversations yet">
        Conversations appear here once someone starts one.
      </ChatState>
    );
  }

  let thread: ReactNode = noneOpen;
  if (taskId) thread = <TaskThread key={taskId} taskId={taskId} view={view} onView={setView} />;
  else if (id) thread = <OrchestratorThread key={id} conversationId={id} view={view} onView={setView} />;

  return (
    <div className="chat-page">
      <PageHeader title="Chat" />
      {startError ? (
        <p className="error-line" role="alert">
          {startError}
        </p>
      ) : null}
      <ErrorLine error={conversations.error} />
      <div className={cx('chat-layout', !showList && 'no-list')}>
        {showList ? (
          <Sidebar
            compact={open}
            canOperate={canOperate}
            activeId={id}
            activeTaskId={taskId}
            orchestrator={(conversations.data ?? []).filter((c) => !c.task_id)}
            agents={agents.data ?? []}
            loading={conversations.isPending}
          />
        ) : null}
        <section className="thread">{thread}</section>
      </div>
      <MessageRail />
    </div>
  );
}

function Lamp({ state }: { state: ChatLampState }) {
  return <span className={cx('lamp', 'chat-lamp', `chat-lamp-${state.lamp}`, state.lamp === 'run' && 'pulse')} aria-hidden="true" />;
}

function Sidebar({
  compact,
  canOperate,
  activeId,
  activeTaskId,
  orchestrator,
  agents,
  loading,
}: {
  compact: boolean;
  canOperate: boolean;
  activeId?: string;
  activeTaskId?: string;
  orchestrator: { id: string; title: string; busy: boolean; updated_at: string }[];
  agents: AgentConversation[];
  loading: boolean;
}) {
  return (
    <aside className={cx('conv-list', compact && 'compact')} aria-label="Conversations">
      {canOperate ? (
        <Link to="/chat" className="btn btn-primary conv-new">
          + New conversation
        </Link>
      ) : null}
      <h2 className="conv-heading">Orchestrator</h2>
      {loading ? <ChatSkeleton variant="list" label="Loading conversations" /> : null}
      <ul className="conv-group">
        {orchestrator.map((conversation) => (
          <li key={conversation.id}>
            <Link to={`/chat/${conversation.id}`} className={cx('conv-item', conversation.id === activeId && 'active')}>
              <span className="conv-title">{conversation.title || 'Untitled'}</span>
              <span className="conv-meta muted small">
                {conversation.busy ? 'working · ' : ''}
                <ConvTime value={conversation.updated_at} />
              </span>
            </Link>
          </li>
        ))}
      </ul>
      {agents.length > 0 ? (
        <>
          <h2 className="conv-heading conv-heading-quiet">Agents</h2>
          <div className="conv-group">
            {agents.map((agent) => {
              const lamp = agentLamp(agent);
              return (
                <div key={agent.task_id} className="conv-agent-group">
                  <p className="conv-task" title={agent.title}>
                    {agent.title}
                  </p>
                  <Link
                    to={`/chat/task/${agent.task_id}`}
                    className={cx('conv-agent', agent.task_id === activeTaskId && 'active')}
                  >
                    <Lamp state={lamp} />
                    <span className="conv-agent-role">{agentLabel(agent.role)}</span>
                    <span className="conv-agent-title">{agent.title}</span>
                    <span className="conv-agent-state muted small">{lamp.label}</span>
                  </Link>
                </div>
              );
            })}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function ConvTime({ value }: { value: string }) {
  const now = useNow();
  const date = parseTime(value);
  return <>{date ? formatRelative(date, now) : ''}</>;
}

/** The start of a new chat: the heading, the composer and the example prompts. */
interface StartRequest {
  text: string;
  settings: ConversationSettings;
  /** Files to upload to the new conversation along with the message. */
  files: File[];
}

function StartChat({ starting, onStart }: { starting: boolean; onStart: (start: StartRequest) => Promise<void> }) {
  const [draft, setDraft, clearDraft] = useDraft('new');
  const [settings, setSettings] = useState<ConversationSettings>({});
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const config = useConfig();
  const attach = useLocalAttachments(config.data?.chat?.attachments?.max_per_message || DEFAULT_MAX_PER_MESSAGE);

  const submit = async () => {
    const text = draft.trim();
    if ((!text && attach.ids.length === 0) || starting) return;
    try {
      await onStart({ text, settings, files: attach.files() });
      clearDraft();
    } catch {
      // The error is shown in the line above the page; the draft stays for another try.
    }
  };

  return (
    <div className="chat-start">
      <ChatState icon="chat" title="Talk to the orchestrator">
        It can create tasks, check on work in flight and answer questions about your projects. It acts through the
        same API as you, so it can never change settings or users.
      </ChatState>
      <Composer
        textareaRef={textareaRef}
        draft={draft}
        onDraft={setDraft}
        onSend={() => void submit()}
        disabled={starting}
        sendDisabled={starting || (draft.trim() === '' && attach.ids.length === 0)}
        attach={attach}
        placeholder="Message the orchestrator…"
        label="Message the orchestrator"
        leftControls={<DraftComposerSettings side="permission" settings={settings} onChange={setSettings} />}
        rightControls={<DraftComposerSettings side="model" settings={settings} onChange={setSettings} />}
        autoFocus
      />
      <div className="examples-row">
        {EXAMPLES.map((example) => (
          <button
            key={example}
            type="button"
            className="chip"
            onClick={() => {
              setDraft(example);
              textareaRef.current?.focus();
            }}
          >
            {example}
          </button>
        ))}
      </div>
    </div>
  );
}

// ---- pieces shared by both kinds of thread ----

function LoadFailed({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <ChatState
      icon="error"
      title="Couldn't load this conversation"
      action={
        <Button variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      }
    >
      {errorMessage(error)}
    </ChatState>
  );
}

function ThreadHead({
  who,
  sub,
  extra,
  view,
  onView,
}: {
  who: string;
  sub: ReactNode;
  extra?: ReactNode;
  view: TranscriptView;
  onView: (v: TranscriptView) => void;
}) {
  return (
    <header className="thread-head">
      <div className="thread-who">
        <h2 className="thread-title">{who}</h2>
        <div className="thread-sub muted small">{sub}</div>
      </div>
      <div className="thread-tools">
        {extra}
        <ViewToggle view={view} onChange={onView} />
      </div>
    </header>
  );
}

/** The activity line for a thread: the live run while a turn runs, else the last run. */
function TurnActivity({
  runId,
  working,
  label,
  lines,
  attention,
}: {
  runId?: string;
  working: boolean;
  label: string;
  lines?: LogLine[];
  attention?: Attention;
}) {
  const run = useRun(runId ?? '');
  return <ActivityLine run={run.data} working={working} label={label} lines={lines} attention={attention} />;
}

/** Edit and Cancel for one queued message, shown in its note. */
function QueuedActions({ message }: { message: ChatMessage | undefined }) {
  const edit = useEditMessage();
  const cancel = useCancelMessage();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  if (!message) return null;
  const save = async () => {
    const next = text.trim();
    if (!next) return;
    try {
      await edit.mutateAsync({ id: message.id, text: next });
      setEditing(false);
    } catch {
      // The error shows in the dialog; the text stays for another try.
    }
  };
  return (
    <span className="tr-qa">
      {' · '}
      <button
        type="button"
        onClick={() => {
          setText(message.text);
          edit.reset();
          setEditing(true);
        }}
      >
        edit
      </button>
      {' · '}
      <button type="button" disabled={cancel.isPending} onClick={() => cancel.mutate(message.id)}>
        cancel
      </button>
      {cancel.error ? <span className="error-line"> {errorMessage(cancel.error)}</span> : null}
      <Modal open={editing} title="Edit queued message" onClose={() => setEditing(false)}>
        <form
          className="chat-modal-form"
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <label className="sr-only" htmlFor="edit-queued">
            Message
          </label>
          <textarea id="edit-queued" rows={4} value={text} autoFocus onChange={(event) => setText(event.target.value)} />
          <ErrorLine error={edit.error} />
          <div className="chat-modal-actions">
            <Button variant="secondary" onClick={() => setEditing(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={edit.isPending || text.trim() === ''}>
              Save
            </Button>
          </div>
        </form>
      </Modal>
    </span>
  );
}

function TaskChip({ task }: { task: Task }) {
  const lamp = taskLamp(task.status);
  return (
    <div className="tr-chip">
      <Lamp state={lamp} />
      <span>{task.title}</span>
      <span className="tr-chip-status">· {lamp.label}</span>
      <Link to={`/chat/task/${task.id}`}>open agent</Link>
      <Link to={`/tasks/${task.id}`}>task</Link>
    </div>
  );
}

function SendNow({ onClick, pending }: { onClick: () => void; pending: boolean }) {
  return (
    <div className="send-now">
      <span className="muted small">No run is working, so queued messages are waiting for you.</span>
      <Button variant="secondary" disabled={pending} onClick={onClick}>
        Send now
      </Button>
    </div>
  );
}

const uniq = (ids: (string | undefined)[]): string[] => [...new Set(ids.filter((x): x is string => !!x))];

// ---- the orchestrator's thread ----

function OrchestratorThread({
  conversationId,
  view,
  onView,
}: {
  conversationId: string;
  view: TranscriptView;
  onView: (v: TranscriptView) => void;
}) {
  const conversation = useConversation(conversationId);
  const send = useSendMessage(conversationId);
  const flush = useFlushQueued();
  const stop = useStopRun();
  const tasks = useTasks({ limit: 200 });
  const navigate = useNavigate();
  const location = useLocation();
  const [draft, setDraft, clearDraft] = useDraft(conversationId);
  const [sendError, setSendError] = useState<string | null>(null);
  const [stoppingRun, setStoppingRun] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const canOperate = useCan('operate');
  const autoSent = useRef(false);

  const conv = conversation.data?.conversation;
  const messages = conversation.data?.messages ?? [];
  const liveRunId = conv?.busy ? conv.active_run_id || undefined : undefined;
  const runIds = uniq([...messages.map((m) => m.run_id), liveRunId]);
  const lastRunId = runIds[runIds.length - 1];
  const logs = useRunLogsMap(runIds, liveRunId);
  const lastRun = useRun(lastRunId ?? '');
  const runs = useRunsMap(runIds);
  const entries = orchestratorEntries(messages, logs, runs);
  const queued = messages.filter((m) => m.queued && m.role === 'user').length;
  const working = !!conv?.busy;
  const byId = Object.fromEntries(messages.map((m) => [m.id, m]));

  const stick = useStickToBottom(entries.length, conversationId);
  // Stop is "Stopping…" from the click until this run ends (liveRunId changes).
  const stopRun = () => {
    if (!liveRunId) return;
    setStoppingRun(liveRunId);
    stop.mutate(liveRunId, { onError: () => setStoppingRun(null) });
  };

  const config = useConfig();
  const attach = useAttachmentUploads(
    conversationId,
    config.data?.chat?.attachments?.max_per_message || DEFAULT_MAX_PER_MESSAGE,
  );

  const sendText = async (text: string, attachments: string[] = []) => {
    setSendError(null);
    try {
      stick.followNow();
      await send.mutateAsync({ text, attachments });
      clearDraft();
      attach.clear();
    } catch (err) {
      setSendError(errorMessage(err));
    }
  };

  // A message handed over by StartChat is sent once, when this conversation first opens. Its files
  // upload first and the send waits for them. The draft stays in the composer if the send fails.
  const handover = useRef(location.state as { message?: string; files?: File[] } | null);
  const [handoverPending, setHandoverPending] = useState(!!handover.current?.message || !!handover.current?.files?.length);
  useEffect(() => {
    const first = handover.current;
    if (!first || autoSent.current) return;
    autoSent.current = true;
    // Drop the handed-over message from history so a reload does not send it again.
    navigate(location.pathname, { replace: true, state: null });
    if (first.message) setDraft(first.message);
    if (first.files?.length) attach.add(first.files);
    else if (first.message) void sendText(first.message);
    else setHandoverPending(false);
    // Runs once per mount: sendText changes every render and the ref guards the rest.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    const first = handover.current;
    if (!handoverPending || !first?.files?.length || attach.items.length === 0 || attach.uploading) return;
    setHandoverPending(false);
    // A file that failed to upload stays as a chip with its error; nothing is sent until it is dealt with.
    if (attach.items.some((it) => it.status === 'error')) return;
    void sendText(first.message ?? '', attach.ids);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [handoverPending, attach.uploading, attach.items]);

  if (conversation.isError) {
    // The API answers 404 for an id that was deleted or never existed: say so, and offer a fresh start.
    if (conversation.error instanceof ApiError && conversation.error.status === 404) {
      return (
        <ChatState
          icon="missing"
          title="This conversation no longer exists"
          action={
            <Link to="/chat" className="btn btn-secondary">
              Start a new conversation
            </Link>
          }
        >
          It was deleted, or the link is wrong.
        </ChatState>
      );
    }
    return <LoadFailed error={conversation.error} onRetry={() => void conversation.refetch()} />;
  }
  if (!conv) return <ChatSkeleton />;

  const state = composerState({ canSend: true, viewer: !canOperate, working, queued });
  const run = lastRun.data;
  const sub = [run?.harness, run?.model, conv.title || 'Untitled'].filter(Boolean).join(' · ');
  const createdBy = (runId: string): ReactNode => {
    const made = (tasks.data ?? []).filter((t) => t.created_by_run === runId);
    return made.length > 0 ? made.map((t) => <TaskChip key={t.id} task={t} />) : null;
  };

  return (
    <>
      <ThreadHead who="Orchestrator" sub={sub} view={view} onView={onView} />
      <div className="messages" ref={stick.contentRef}>
        {entries.length === 0 && canOperate ? (
          <div className="examples">
            <p className="muted">Ask anything, or try one of these:</p>
            {EXAMPLES.map((example) => (
              <button
                key={example}
                type="button"
                className="chip"
                onClick={() => {
                  setDraft(example);
                  textareaRef.current?.focus();
                }}
              >
                {example}
              </button>
            ))}
          </div>
        ) : null}
        <Transcript
          entries={entries}
          view={view}
          agentLabel="Orchestrator"
          afterRun={createdBy}
          queuedActions={canOperate ? (mid) => <QueuedActions message={byId[mid]} /> : undefined}
          liveRunId={liveRunId}
          runTokens={tokensByRun(Object.values(runs))}
        />
        <TurnActivity
          runId={liveRunId ?? lastRunId}
          working={working}
          label="Orchestrator"
          lines={liveRunId ? logs[liveRunId] : undefined}
        />
        {canOperate && queued > 0 && !working ? (
          <SendNow pending={flush.isPending} onClick={() => flush.mutate({ conversationId })} />
        ) : null}
      </div>
      <ComposerArea
        stick={stick}
        state={state}
        working={working}
        draft={draft}
        onDraft={setDraft}
        onSend={() => {
          if (attach.uploading || (!draft.trim() && attach.ids.length === 0)) return;
          void sendText(draft.trim(), attach.ids);
        }}
        sending={send.isPending}
        attach={attach}
        onStop={stopRun}
        stopping={!!liveRunId && stoppingRun === liveRunId}
        textareaRef={textareaRef}
        label="Message the orchestrator"
        modelLabel={canOperate ? undefined : run?.model || run?.harness}
        leftControls={canOperate ? <ComposerSettings conversationId={conversationId} side="permission" /> : undefined}
        rightControls={
          canOperate ? (
            <>
              <ComposerSettings conversationId={conversationId} side="model" />
              <ContextRing fraction={contextFraction(run)} />
            </>
          ) : undefined
        }
      />
      <ErrorLine error={stop.error} />
      <ErrorLine error={flush.error} />
      {sendError ? (
        <p className="error-line" role="alert">
          {sendError}
        </p>
      ) : null}
    </>
  );
}

// ---- a task agent's thread ----

function TaskThread({
  taskId,
  view,
  onView,
}: {
  taskId: string;
  view: TranscriptView;
  onView: (v: TranscriptView) => void;
}) {
  const query = useTaskConversation(taskId);
  const send = useSendTaskMessage(taskId);
  const flush = useFlushQueued();
  const stop = useStopRun();
  const reopen = useReopenTaskConversation(taskId);
  const canOperate = useCan('operate');
  const [draft, setDraft, clearDraft] = useDraft(`task:${taskId}`);
  const [sendError, setSendError] = useState<string | null>(null);
  const [stoppingRun, setStoppingRun] = useState<string | null>(null);
  const [closing, setClosing] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const conv = query.data;
  const liveRunId = conv?.live_run_id || undefined;
  const logs = useRunLogsMap(
    (conv?.runs ?? []).map((r) => r.id),
    liveRunId,
  );
  const entries = conv ? taskEntries(conv, logs) : [];
  const messages = conv?.messages ?? [];
  const queued = messages.filter((m) => m.queued && m.role === 'user').length;
  const byId = Object.fromEntries(messages.map((m) => [m.id, m]));

  const stick = useStickToBottom(entries.length, taskId);
  // Stop is "Stopping…" from the click until this run ends (liveRunId changes).
  const stopRun = () => {
    if (!liveRunId) return;
    setStoppingRun(liveRunId);
    stop.mutate(liveRunId, { onError: () => setStoppingRun(null) });
  };

  if (query.isError) {
    if (query.error instanceof ApiError && query.error.status === 404) {
      return (
        <ChatState
          icon="missing"
          title="This task has no conversation"
          action={
            <Link to="/chat" className="btn btn-secondary">
              Back to chat
            </Link>
          }
        >
          The agent has not said anything yet, or the task is gone.
        </ChatState>
      );
    }
    return <LoadFailed error={query.error} onRetry={() => void query.refetch()} />;
  }
  if (!conv) return <ChatSkeleton />;

  const { task } = conv;
  const role = agentLabel(task.role) || 'agent';
  const closedAt = conv.conversation?.closed_at;
  const closed = !!closedAt;
  const lastRun = conv.runs[conv.runs.length - 1];
  // Owners pick model, effort and permissions once the task has a conversation to store them on.
  const settingsId = canOperate ? conv.conversation?.id : undefined;
  const sub = (
    <>
      {[lastRun?.harness, lastRun?.model].filter(Boolean).join(' · ')}
      {lastRun ? ' · ' : ''}task <Link to={`/tasks/${task.id}`}>{task.title}</Link>
      {task.branch ? <> · branch <span className="mono">{task.branch}</span></> : null}
      {' · '}
      {taskLamp(task.status).label}
    </>
  );
  const state = composerState({
    canSend: conv.can_send,
    reason: conv.reason,
    viewer: !canOperate,
    working: !!liveRunId,
    queued,
    role,
  });

  const sendText = async (text: string) => {
    setSendError(null);
    try {
      stick.followNow();
      await send.mutateAsync(text);
      clearDraft();
    } catch (err) {
      // A 409 carries the reason the agent cannot be messaged.
      setSendError(errorMessage(err));
    }
  };

  const closeButton =
    canOperate && !closed ? (
      <Button variant="secondary" onClick={() => setClosing(true)}>
        Close
      </Button>
    ) : null;

  return (
    <>
      <ThreadHead who={capitalise(role)} sub={sub} extra={closeButton} view={view} onView={onView} />
      <div className="messages" ref={stick.contentRef}>
        <Transcript
          entries={entries}
          view={view}
          agentLabel={capitalise(role)}
          queuedActions={canOperate ? (mid) => <QueuedActions message={byId[mid]} /> : undefined}
          liveRunId={liveRunId}
          runTokens={tokensByRun(conv?.runs ?? [])}
        />
        <TurnActivity
          runId={liveRunId ?? conv?.runs?.[conv.runs.length - 1]?.id}
          working={!!liveRunId}
          label={capitalise(role)}
          lines={liveRunId ? logs[liveRunId] : undefined}
          attention={attentionFor(task.status)}
        />
        {canOperate && queued > 0 && !liveRunId && !closed ? (
          <SendNow pending={flush.isPending} onClick={() => flush.mutate({ taskId })} />
        ) : null}
      </div>
      {closed ? (
        <div className="closed-note">
          <p>
            Closed by {conv.conversation?.closed_by}: {conv.conversation?.closed_reason}
          </p>
          {canOperate ? (
            <Button variant="secondary" disabled={reopen.isPending} onClick={() => reopen.mutate()}>
              Reopen
            </Button>
          ) : null}
        </div>
      ) : (
        <>
          {state.enabled ? (
            <div className="chat-hint">
              You&apos;re talking directly to this task&apos;s agent. For anything beyond this task,{' '}
              <Link to="/chat">ask the orchestrator</Link>.
            </div>
          ) : null}
          <ComposerArea
            stick={stick}
            state={state}
            working={!!liveRunId}
            draft={draft}
            onDraft={setDraft}
            onSend={() => draft.trim() && void sendText(draft.trim())}
            sending={send.isPending}
            onStop={stopRun}
            stopping={!!liveRunId && stoppingRun === liveRunId}
            textareaRef={textareaRef}
            label={`Message the ${role}`}
            modelLabel={
              settingsId
                ? role
                : [role, lastRun?.model || lastRun?.harness, lastRun?.effort, lastRun?.permission_mode]
                    .filter(Boolean)
                    .join(' · ')
            }
            leftControls={settingsId ? <ComposerSettings conversationId={settingsId} side="permission" /> : undefined}
            rightControls={
              <>
                {settingsId ? <ComposerSettings conversationId={settingsId} side="model" /> : null}
                <ContextRing fraction={contextFraction(lastRun)} />
              </>
            }
          />
        </>
      )}
      <ErrorLine error={reopen.error} />
      <ErrorLine error={stop.error} />
      <ErrorLine error={flush.error} />
      {sendError ? (
        <p className="error-line" role="alert">
          {sendError}
        </p>
      ) : null}
      <CloseModal open={closing} taskId={taskId} onClose={() => setClosing(false)} />
    </>
  );
}

function CloseModal({ open, taskId, onClose }: { open: boolean; taskId: string; onClose: () => void }) {
  const close = useCloseTaskConversation(taskId);
  const [reason, setReason] = useState('');
  const submit = async () => {
    const text = reason.trim();
    if (!text) return;
    try {
      await close.mutateAsync(text);
      setReason('');
      onClose();
    } catch {
      // The error shows in the dialog; the reason stays for another try.
    }
  };
  return (
    <Modal open={open} title="Close this conversation" onClose={onClose}>
      <form
        className="chat-modal-form"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <p className="muted small">
          The agent can no longer be messaged and queued messages are never sent. The task itself is not changed. You can
          reopen it later.
        </p>
        <label htmlFor="close-reason">Reason</label>
        <textarea id="close-reason" rows={3} value={reason} autoFocus onChange={(event) => setReason(event.target.value)} />
        <ErrorLine error={close.error} />
        <div className="chat-modal-actions">
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={close.isPending || reason.trim() === ''}>
            Close conversation
          </Button>
        </div>
      </form>
    </Modal>
  );
}
