// The Add project dialog (Direction A of docs/proposals/add-project). It picks a repository,
// checks it on the server (a copy, the default branch, pull requests, guessed commands), shows the
// result for the person to look over, and only then saves the project and offers a first task.
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { ConfigApiError, discardInspect, inspectProject, listGithubRepos } from '../api';
import { keys } from '../keys';
import { copyText } from '../lib/clipboard';
import {
  addBlocker,
  addProjectRequest,
  FIRST_TASK_BODY,
  firstTaskTitle,
  guessedFrom,
  initialValues,
  POLICY_SENTENCE,
  type AddProjectValues,
} from '../lib/projectForm';
import { useCreateProject, useCreateTask } from '../queries';
import type { InspectResult, InspectStep, InspectStepName, InspectStepState } from '../types';
import { ErrorLine } from './ErrorLine';
import { Modal } from './Modal';
import { RelTime } from './RelTime';
import './AddProjectDialog.css';

type Phase = 'pick' | 'checking' | 'review' | 'ready';

/** Commands start with one empty row each when nothing was guessed, so there is a place to type. */
function startValues(r: InspectResult): AddProjectValues {
  const v = initialValues(r);
  return { ...v, setup: v.setup.length ? v.setup : [''], checks: v.checks.length ? v.checks : [''] };
}

/** "a", "a and b", "a, b and c": the items as one line of text, with the last joiner given. */
function joinList(items: ReactNode[], last = ' and '): ReactNode {
  return items.map((item, i) => (
    <span key={i}>
      {i === 0 ? null : i === items.length - 1 ? last : ', '}
      {item}
    </span>
  ));
}

export function AddProjectDialog({ open, onClose, existing }: { open: boolean; onClose: () => void; existing: string[] }) {
  const navigate = useNavigate();
  const repos = useQuery({ queryKey: keys.githubRepos, queryFn: listGithubRepos, enabled: open });
  const create = useCreateProject();
  const startTask = useCreateTask();
  const [phase, setPhase] = useState<Phase>('pick');
  const [query, setQuery] = useState('');
  const [target, setTarget] = useState('');
  const [steps, setSteps] = useState<InspectStep[]>([]);
  const [result, setResult] = useState<InspectResult | null>(null);
  const [values, setValues] = useState<AddProjectValues | null>(null);
  const [checkError, setCheckError] = useState<unknown>(null);
  const attempt = useRef<AbortController | null>(null);
  const repoId = useId();
  const hintId = useId();
  const signedOut = repos.data !== undefined && !repos.data.gh.signed_in;

  // A check still running when the dialog goes away is abandoned.
  useEffect(() => () => attempt.current?.abort(), []);

  const latest = (name: InspectStepName): InspectStep | undefined => [...steps].reverse().find((s) => s.name === name);

  const start = (text: string) => {
    const repo = text.trim();
    if (!repo) return;
    attempt.current?.abort();
    const ctrl = new AbortController();
    attempt.current = ctrl;
    setTarget(repo);
    setPhase('checking');
    setSteps([]);
    setResult(null);
    setValues(null);
    setCheckError(null);
    inspectProject(repo, (step) => setSteps((s) => [...s.filter((x) => x.name !== step.name), step]), ctrl.signal)
      .then((r) => {
        if (ctrl.signal.aborted) return;
        setResult(r);
        setValues(startValues(r));
        setPhase('review');
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        setCheckError(err);
        setPhase('pick');
      });
  };

  /** Back to the picker. A copy already made for this check is dropped, since nothing is saved. */
  const change = () => {
    attempt.current?.abort();
    if (result?.copy.ok) discardInspect(result.name).catch(() => {});
    setPhase('pick');
    setSteps([]);
    setResult(null);
    setValues(null);
    setCheckError(null);
  };

  const close = () => {
    if (phase !== 'ready' && result?.copy.ok) discardInspect(result.name).catch(() => {});
    attempt.current?.abort();
    attempt.current = null;
    setPhase('pick');
    setQuery('');
    setTarget('');
    setSteps([]);
    setResult(null);
    setValues(null);
    setCheckError(null);
    create.reset();
    startTask.reset();
    onClose();
  };

  const patch = (change: Partial<AddProjectValues>) => setValues((v) => (v ? { ...v, ...change } : v));
  // Typing in a command means the person wants commands, so "No setup or checks" is turned off.
  const setCommand = (kind: 'setup' | 'checks', index: number, text: string) =>
    setValues((v) => (v ? { ...v, none: false, [kind]: v[kind].map((c, i) => (i === index ? text : c)) } : v));
  const removeCommand = (kind: 'setup' | 'checks', index: number) =>
    setValues((v) => (v ? { ...v, [kind]: v[kind].filter((_, i) => i !== index) } : v));
  const addCommand = (kind: 'setup' | 'checks') => setValues((v) => (v ? { ...v, [kind]: [...v[kind], ''] } : v));

  const add = () => {
    if (!result || !values) return;
    create.mutate(addProjectRequest(result, values), { onSuccess: () => setPhase('ready') });
  };

  const name = values?.name.trim() ?? '';
  const blocker = result && values ? addBlocker(result, values, existing) : null;
  const problems = create.error instanceof ConfigApiError ? create.error.problems : [];

  const openFirstTask = () =>
    startTask.mutate(
      {
        project: name,
        kind: 'question',
        title: firstTaskTitle(name),
        body: FIRST_TASK_BODY,
        role: '',
        priority: 0,
        parent_id: '',
        source: 'api',
      },
      {
        onSuccess: (task) => {
          close();
          navigate(`/tasks/${task.id}`);
        },
      },
    );

  const writeOwn = () => {
    close();
    navigate(`/tasks?new=1&project=${encodeURIComponent(name)}`);
  };

  return (
    <Modal open={open} title={phase === 'ready' ? `${name} is added` : 'Add project'} onClose={close}>
      {phase === 'pick' ? (
        <>
          <p className="ap-lede">
            Pick the repository onett should work on. onett checks it and fills in the rest; you look it over before
            anything is saved.
          </p>
          {repos.data && !repos.data.gh.signed_in ? (
            <div className="callout callout-info" role="note">
              <strong>The GitHub CLI is not signed in on this machine.</strong> onett uses it to list your repositories and
              to open pull requests, so it cannot add a project until it is. On the machine onett runs on
              {repos.data.gh.host ? ` (${repos.data.gh.host})` : ''}, run:
              <div className="row-actions" style={{ marginTop: 8 }}>
                <code className="ap-code">gh auth login</code>
                <button type="button" className="link-button" onClick={() => void copyText('gh auth login')}>
                  Copy
                </button>
                <button type="button" className="btn btn-secondary" onClick={() => void repos.refetch()}>
                  Check again
                </button>
              </div>
            </div>
          ) : null}
          <div className="field">
            {/* The label holds only its text, so the input's name is exactly "Repository". */}
            <label className="field-control" htmlFor={repoId}>
              <span className="field-label">Repository</span>
            </label>
            <span className="ap-input-row">
              <input
                id={repoId}
                type="text"
                autoFocus
                aria-describedby={signedOut ? hintId : undefined}
                placeholder="Search, or paste owner/name or a GitHub link"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    start(query);
                  }
                }}
              />
              <button type="button" className="btn btn-secondary" disabled={!query.trim()} onClick={() => start(query)}>
                Check
              </button>
            </span>
            {signedOut ? (
              <span id={hintId} className="field-hint">
                owner/name or a GitHub link. You can check a public repository now; Add stays off until the GitHub CLI is
                signed in.
              </span>
            ) : null}
          </div>
          <ErrorLine error={checkError} />
          <ErrorLine error={repos.error} />
          <RepoList
            repos={repos.data?.repos ?? []}
            query={query}
            onPick={start}
          />
          {repos.data?.gh.error ? (
            <p className="ap-source">Could not list your repositories: {repos.data.gh.error}. Type owner/name above.</p>
          ) : repos.data?.gh.signed_in ? (
            <p className="ap-source">
              Your repositories, most recently pushed first, listed with the GitHub CLI on this machine (signed in as{' '}
              {repos.data.gh.user}). Not here? Type owner/name above.
            </p>
          ) : null}
          <div className="ap-foot">
            <button type="button" className="btn btn-ghost" onClick={close}>
              Cancel
            </button>
          </div>
        </>
      ) : phase === 'ready' && values ? (
        <ReadyBody
          name={name}
          values={values}
          onStart={openFirstTask}
          onWriteOwn={writeOwn}
          onDone={close}
          pending={startTask.isPending}
          error={startTask.error}
        />
      ) : (
        <>
          <Picked repo={result?.repo ?? target} private={result ? result.private : null} onChange={change} />
          <ChecksList
            result={result}
            latest={latest}
            onCheckAgain={() => start(result?.repo ?? target)}
            onChange={change}
            onRetry={start}
            host={repos.data?.gh.host ?? ''}
          />
          {result?.copy.ok && values ? (
            <>
              <h3 className="ap-heading">Before every task</h3>
              <Guess
                result={result}
                values={values}
                patch={patch}
                setCommand={setCommand}
                removeCommand={removeCommand}
                addCommand={addCommand}
              />
              <h3 className="ap-heading">Who merges</h3>
              <p className="ap-policy">{POLICY_SENTENCE[values.policy]}</p>
              <Advanced result={result} values={values} patch={patch} />
            </>
          ) : null}
          {problems.length > 0 ? (
            <ul className="error-line" role="alert">
              {problems.map((p, i) => (
                <li key={i}>
                  {p.path ? <span className="mono">{p.path}: </span> : null}
                  {p.message}
                </li>
              ))}
            </ul>
          ) : (
            <ErrorLine error={create.error} />
          )}
          <div className="ap-foot">
            {result && values ? (
              <button type="button" className="btn btn-primary" disabled={!!blocker || create.isPending} onClick={add}>
                Add {name || result.name}
              </button>
            ) : null}
            <button type="button" className="btn btn-ghost" onClick={close}>
              Cancel
            </button>
            {blocker ? <span className="ap-why">{blocker}</span> : null}
          </div>
        </>
      )}
    </Modal>
  );
}

/** The folded Advanced section: name, branch, who merges, and instructions for agents. */
function Advanced({
  result,
  values,
  patch,
}: {
  result: InspectResult;
  values: AddProjectValues;
  patch: (change: Partial<AddProjectValues>) => void;
}) {
  const branchHint = !result.default_branch
    ? 'The branch agents start from.'
    : result.branch_from === 'github'
      ? <>The branch agents start from. GitHub says <span className="ap-mono">{result.default_branch}</span>.</>
      : <>The branch agents start from. The repository says <span className="ap-mono">{result.default_branch}</span>.</>;
  return (
    <details className="ap-adv">
      <summary>
        Advanced
        <span>
          name {values.name} · branch {values.branch} · policy {values.policy}
        </span>
      </summary>
      <div className="form-stack" style={{ marginTop: 12 }}>
        <div className="field">
          <label className="field-control">
            <span className="field-label">Name</span>
            <input type="text" value={values.name} onChange={(e) => patch({ name: e.target.value })} />
          </label>
          <span className="field-hint">How onett refers to it. Taken from the repository's name.</span>
        </div>
        <div className="field">
          <label className="field-control">
            <span className="field-label">Branch</span>
            <input type="text" className="ap-mono" value={values.branch} onChange={(e) => patch({ branch: e.target.value })} />
          </label>
          <span className="field-hint">{branchHint}</span>
        </div>
        <div className="field">
          <span className="field-label">Who merges</span>
          <div className="ap-choice" role="radiogroup" aria-label="Who merges" style={{ marginTop: 0 }}>
            <label>
              <input
                type="radio"
                name="ap-policy"
                checked={values.policy === 'supervised'}
                onChange={() => patch({ policy: 'supervised' })}
              />
              <span>
                Small changes merge on their own{' '}
                <small>
                  Low-risk changes merge after review and CI; the rest wait for you. Policy <span className="ap-mono">supervised</span>.
                </small>
              </span>
            </label>
            <label>
              <input
                type="radio"
                name="ap-policy"
                checked={values.policy === 'default'}
                onChange={() => patch({ policy: 'default' })}
              />
              <span>
                Everything merges on its own{' '}
                <small>
                  Any change merges once the reviewing agent approves and CI passes. Policy <span className="ap-mono">default</span>.
                </small>
              </span>
            </label>
          </div>
        </div>
        <div className="field">
          <label className="field-control">
            <span className="field-label">Instructions for agents</span>
            <textarea
              rows={3}
              placeholder="Optional. Most projects keep these in AGENTS.md or CLAUDE.md in the repository, which agents read anyway."
              value={values.instructions}
              onChange={(e) => patch({ instructions: e.target.value })}
            />
          </label>
        </div>
      </div>
    </details>
  );
}

/** The repositories the owner can pick, filtered by what is typed. At most eight. */
function RepoList({
  repos,
  query,
  onPick,
}: {
  repos: { full_name: string; private: boolean; description: string; pushed_at: string; project: string | null }[];
  query: string;
  onPick: (repo: string) => void;
}) {
  const needle = query.trim().toLowerCase();
  const shown = repos
    .filter((r) => !needle || r.full_name.toLowerCase().includes(needle) || r.description.toLowerCase().includes(needle))
    .slice(0, 8);
  if (shown.length === 0) return null;
  return (
    <ul className="ap-repos" aria-label="Your repositories">
      {shown.map((r) => (
        <li key={r.full_name}>
          <button type="button" className="ap-repo" disabled={r.project !== null} onClick={() => onPick(r.full_name)}>
            <span className="ap-repo-name">{r.full_name}</span>
            <RelTime className="ap-repo-when" value={r.pushed_at} />
            <span className="ap-repo-desc">
              {r.project !== null ? <span className="tag">added as {r.project}</span> : null}
              <span className="tag">{r.private ? 'private' : 'public'}</span>
              {r.description || 'No description'}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

function Picked({ repo, private: isPrivate, onChange }: { repo: string; private: boolean | null; onChange: () => void }) {
  return (
    <div className="ap-picked">
      <strong>{repo}</strong>
      {isPrivate ? <span className="tag">private</span> : null}
      <button type="button" className="link-button" onClick={onChange}>
        Change
      </button>
    </div>
  );
}

const STATE: Record<'waiting' | InspectStepState, { label: string; cls: string; lamp: string }> = {
  waiting: { label: 'Waiting', cls: 'is-wait', lamp: 'lamp-off' },
  running: { label: 'Checking', cls: 'is-wait', lamp: 'lamp-run pulse' },
  done: { label: 'Done', cls: 'is-done', lamp: 'lamp-run' },
  failed: { label: 'Stopped', cls: 'is-stop', lamp: 'lamp-stop' },
  skipped: { label: 'Skipped', cls: 'is-wait', lamp: 'lamp-off' },
};

function CheckRow({ state, title, detail, body }: { state: keyof typeof STATE; title: ReactNode; detail?: ReactNode; body?: ReactNode }) {
  const s = STATE[state];
  return (
    <li className="ap-check">
      <span className={`ap-state ${s.cls}`}>
        <span className={`lamp ${s.lamp}`} aria-hidden="true" />
        {s.label}
      </span>
      <span className="ap-check-title">{title}</span>
      {detail ? <span className="ap-check-detail">{detail}</span> : null}
      {body ? <div className="ap-check-body">{body}</div> : null}
    </li>
  );
}

/** The three checks: copy, default branch, pull requests. Each row reads the latest step for it. */
function ChecksList({
  result,
  latest,
  onCheckAgain,
  onChange,
  onRetry,
  host,
}: {
  result: InspectResult | null;
  latest: (name: InspectStepName) => InspectStep | undefined;
  onCheckAgain: () => void;
  onChange: () => void;
  onRetry: (repo: string) => void;
  host: string;
}) {
  const copy = latest('copy');
  const branch = latest('branch');
  const pr = latest('pull_requests');
  const copyFailed = copy?.state === 'failed' || (result !== null && !result.copy.ok);

  let copyRow: ReactNode;
  if (copyFailed) {
    const error = result?.copy.error ?? copy?.detail ?? '';
    const who = result?.pull_requests.gh_user ?? '';
    copyRow = (
      <CheckRow
        state="failed"
        title="Could not copy the repository"
        body={
          <>
            <span className="ap-check-detail" style={{ display: 'block' }}>
              {result?.github ? 'GitHub said:' : 'git said:'}
            </span>
            <div className="ap-said">{error}</div>
            <span className="ap-check-detail" style={{ display: 'block' }}>
              {result?.github && who
                ? `Either the name is wrong, or the GitHub account signed in on this machine (${who}) cannot read it.`
                : 'Check the name, and that this machine can read the repository.'}
              {result?.suggest ? (
                <>
                  {' '}
                  Did you mean{' '}
                  <button type="button" className="link-button" onClick={() => onRetry(result.suggest)}>
                    {result.suggest}
                  </button>
                  ?
                </>
              ) : null}
            </span>
            <div className="row-actions">
              <button type="button" className="btn btn-secondary" onClick={onCheckAgain}>
                Check again
              </button>
              <button type="button" className="btn btn-secondary" onClick={onChange}>
                Change repository
              </button>
            </div>
          </>
        }
      />
    );
  } else if (copy?.state === 'done') {
    copyRow = (
      <CheckRow
        state="done"
        title="Copied the repository"
        detail="onett keeps its own copy and gives each task a separate one. Keep working in your own checkout; onett never touches it."
      />
    );
  } else {
    copyRow = (
      <CheckRow
        state={copy?.state === 'running' ? 'running' : 'waiting'}
        title="Copying the repository"
        detail="This can take a while for a large repository."
      />
    );
  }

  let branchRow: ReactNode;
  if (copyFailed) {
    branchRow = <CheckRow state="waiting" title="Default branch" detail="Read after the copy." />;
  } else if (branch?.state === 'failed') {
    branchRow = <CheckRow state="failed" title="Could not read the default branch" detail={branch.detail} />;
  } else if (branch?.state === 'done' && result) {
    branchRow = (
      <CheckRow
        state="done"
        title={
          <>
            Default branch is <span className="ap-mono">{result.default_branch}</span>
          </>
        }
        detail={
          result.branch_from === 'github'
            ? 'Read from GitHub. Agents start from it and open pull requests against it.'
            : 'Read from the repository. Agents start from it.'
        }
      />
    );
  } else if (branch) {
    branchRow = <CheckRow state="running" title="Default branch" detail="Reading it now." />;
  } else {
    branchRow = <CheckRow state="waiting" title="Default branch" detail="Read after the copy." />;
  }

  let prRow: ReactNode;
  if (copyFailed) {
    prRow = <CheckRow state="waiting" title="Pull requests" detail="Checked after the copy." />;
  } else if (pr?.state === 'failed') {
    prRow = (
      <CheckRow
        state="failed"
        title="Cannot open pull requests"
        body={
          <>
            <span className="ap-check-detail" style={{ display: 'block' }}>
              The GitHub CLI is not signed in on this machine. onett uses it to open pull requests. On the machine onett
              runs on{host ? ` (${host})` : ''}, run:
            </span>
            <div className="row-actions">
              <code className="ap-code">gh auth login</code>
              <button type="button" className="link-button" onClick={() => void copyText('gh auth login')}>
                Copy
              </button>
              <button type="button" className="btn btn-secondary" onClick={onCheckAgain}>
                Check again
              </button>
            </div>
          </>
        }
      />
    );
  } else if (pr?.state === 'skipped') {
    prRow = (
      <CheckRow
        state="skipped"
        title="No pull requests"
        detail="Not a GitHub repository: onett can work on it, but cannot open pull requests."
      />
    );
  } else if (pr?.state === 'done' && result) {
    prRow = (
      <CheckRow
        state="done"
        title="Can open pull requests"
        detail={`With the GitHub CLI on this machine, signed in as ${result.pull_requests.gh_user}.`}
      />
    );
  } else if (pr) {
    prRow = <CheckRow state="running" title="Pull requests" detail="Checking the GitHub CLI." />;
  } else {
    prRow = <CheckRow state="waiting" title="Pull requests" detail="Checked after the copy." />;
  }

  return (
    <>
      <h3 className="ap-heading">What onett checked</h3>
      <ul className="ap-checks">
        {copyRow}
        {branchRow}
        {prRow}
      </ul>
    </>
  );
}

/**
 * The commands before every task. Guessed commands are shown with Remove; when none were
 * guessed, or none are left, the explicit choice between running commands and none is shown.
 */
function Guess({
  result,
  values,
  patch,
  setCommand,
  removeCommand,
  addCommand,
}: {
  result: InspectResult;
  values: AddProjectValues;
  patch: (change: Partial<AddProjectValues>) => void;
  setCommand: (kind: 'setup' | 'checks', index: number, text: string) => void;
  removeCommand: (kind: 'setup' | 'checks', index: number) => void;
  addCommand: (kind: 'setup' | 'checks') => void;
}) {
  const guessedAny = result.guess.setup.length > 0 || result.guess.checks.length > 0;
  const given = [...values.setup, ...values.checks].some((c) => c.trim() !== '');
  const card = !given || values.none;
  const files = guessedFrom(result);

  const groups = (remove: boolean) => (
    <>
      <CommandGroup
        kind="setup"
        list={values.setup}
        hint={!card}
        remove={remove}
        placeholder={card ? 'for example ./scripts/bootstrap.sh' : undefined}
        onChange={(i, text) => setCommand('setup', i, text)}
        onRemove={(i) => removeCommand('setup', i)}
        onAdd={() => addCommand('setup')}
      />
      <CommandGroup
        kind="checks"
        list={values.checks}
        hint={!card}
        remove={remove}
        placeholder={card ? 'for example make lint' : undefined}
        onChange={(i, text) => setCommand('checks', i, text)}
        onRemove={(i) => removeCommand('checks', i)}
        onAdd={() => addCommand('checks')}
      />
    </>
  );

  return (
    <>
      {guessedAny && !card ? (
        <p className="ap-lede">
          Guessed from {joinList(files.map((f) => <span key={f} className="ap-mono">{f}</span>))}. Change them if they are
          wrong. They run in each task's copy with the agent's limited environment, never with your credentials.
        </p>
      ) : null}
      {!guessedAny ? (
        <ul className="ap-checks">
          <li className="ap-check">
            <span className="ap-state is-call">
              <span className="lamp lamp-call" aria-hidden="true" />
              Needs you
            </span>
            <span className="ap-check-title">onett found nothing it knows how to install or test</span>
            <span className="ap-check-detail">
              At the top of {result.name}:{' '}
              {result.guess.top_files.length > 0
                ? joinList(
                    result.guess.top_files.map((f) => <span key={f} className="ap-mono">{f}</span>),
                    ', ',
                  )
                : 'nothing'}
              .
            </span>
          </li>
        </ul>
      ) : null}
      {card ? (
        <div className="ap-choice" role="radiogroup" aria-label="Setup and checks">
          <div className="ap-choice-card">
            <label>
              <input type="radio" name="ap-none" checked={!values.none} onChange={() => patch({ none: false })} />
              <span>
                Run these commands
                <small>Setup runs before the agent starts; checks run before onett opens a pull request.</small>
              </span>
            </label>
            <div className="ap-cmds">{groups(false)}</div>
          </div>
          <label>
            <input type="radio" name="ap-none" checked={values.none} onChange={() => patch({ none: true })} />
            <span>
              No setup or checks
              <small>
                Agents get the code with nothing installed, and onett opens pull requests without testing them first. Your
                CI, if any, still runs.
              </small>
            </span>
          </label>
        </div>
      ) : (
        <div className="ap-cmds">{groups(true)}</div>
      )}
    </>
  );
}

function CommandGroup({
  kind,
  list,
  hint,
  remove,
  placeholder,
  onChange,
  onRemove,
  onAdd,
}: {
  kind: 'setup' | 'checks';
  list: string[];
  hint: boolean;
  remove: boolean;
  placeholder?: string;
  onChange: (index: number, text: string) => void;
  onRemove: (index: number) => void;
  onAdd: () => void;
}) {
  const setup = kind === 'setup';
  return (
    <div className="ap-cmd-group">
      <span className="field-label">{setup ? 'Setup' : 'Checks'}</span>
      {hint ? (
        <span className="field-hint">
          {setup
            ? 'Runs before the agent starts, so it can build and test.'
            : 'Run before onett opens a pull request. A failure sends the work back to the agent.'}
        </span>
      ) : null}
      {list.map((cmd, i) => (
        <div className="ap-cmd" key={i}>
          <input
            type="text"
            value={cmd}
            placeholder={placeholder}
            aria-label={`${setup ? 'Setup' : 'Check'} command ${i + 1}`}
            onChange={(e) => onChange(i, e.target.value)}
          />
          {remove ? (
            <button type="button" className="link-button" onClick={() => onRemove(i)}>
              Remove
            </button>
          ) : null}
        </div>
      ))}
      <button type="button" className="link-button" onClick={onAdd}>
        {setup ? 'Add a setup command' : 'Add a check'}
      </button>
    </div>
  );
}

function ReadyBody({
  name,
  values,
  onStart,
  onWriteOwn,
  onDone,
  pending,
  error,
}: {
  name: string;
  values: AddProjectValues;
  onStart: () => void;
  onWriteOwn: () => void;
  onDone: () => void;
  pending: boolean;
  error: unknown;
}) {
  const setup = values.none ? [] : values.setup.map((c) => c.trim()).filter(Boolean);
  const checks = values.none ? [] : values.checks.map((c) => c.trim()).filter(Boolean);
  const mono = (list: string[]) => joinList(list.map((c) => <span className="ap-mono">{c}</span>), ', ');
  return (
    <>
      <p className="ap-state is-done" style={{ margin: '0 0 6px' }}>
        <span className="lamp lamp-run" aria-hidden="true" />
        Ready for work
      </p>
      <p className="ap-summary-line">
        Saved to onett.yaml and loaded. Branch <span className="ap-mono">{values.branch}</span>
        {setup.length > 0 ? <> · setup {mono(setup)}</> : null}
        {checks.length > 0 ? <> · checks {mono(checks)}</> : null}
        {' · '}
        {values.policy === 'supervised' ? 'small changes merge on their own' : 'every change merges on its own'}.
      </p>
      <div className="ap-first">
        <h3>See it work once</h3>
        <p className="ap-lede" style={{ margin: 0 }}>
          A question task asks the agent to read the code and change nothing. It makes a task copy and runs setup and
          checks, so you watch the whole path work before you hand over real work.
        </p>
        <div className="ap-task">
          <strong>{firstTaskTitle(name)}</strong>
          <span>question · {name}</span>
        </div>
        <div className="row-actions">
          <button type="button" className="btn btn-primary" disabled={pending} onClick={onStart}>
            Start this task
          </button>
          <button type="button" className="btn btn-secondary" onClick={onWriteOwn}>
            Write my own task
          </button>
          <button type="button" className="btn btn-ghost" onClick={onDone}>
            Done
          </button>
        </div>
        <ErrorLine error={error} />
      </div>
    </>
  );
}
