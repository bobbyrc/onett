// Guided setup for the ways onett reaches you: a Telegram or Discord bot (a two-way channel) and
// ntfy (a one-way notifier). Each flow stores the token through the write-only secrets API, writes
// the same config entry `onett init` writes, and offers a test send. A token is never shown back
// and the field is emptied as soon as it is saved. If an entry of the kind exists it is edited.
import { useEffect, useState } from 'react';
import { ConfigApiError } from '../../api';
import { usePutSecret, useSaveConfigDocument, useSecrets } from '../../queries';
import type { ConfigDocument, ConfigProblem } from '../../types';
import {
  chatChanges,
  existingUserIds,
  findEntry,
  ntfyChanges,
  parseUserIds,
  randomTopic,
  serverProblem,
  tokenEnvFor,
  type ChatKind,
  type SetupKind,
} from '../../lib/channelSetup';
import { valueProblem } from '../../lib/secrets';
import { cx } from '../../lib/format';
import { Button } from '../Button';
import { Callout } from '../Callout';
import { ErrorLine } from '../ErrorLine';
import { Field } from '../Field';
import { classifySaveFailure } from './saveFailure';
import { SendTestButton } from './SendTestButton';

const FLOWS: { kind: SetupKind; label: string }[] = [
  { kind: 'telegram', label: 'Telegram' },
  { kind: 'discord', label: 'Discord' },
  { kind: 'ntfy', label: 'ntfy' },
];

export function ChannelSetup({ doc }: { doc: ConfigDocument }) {
  const [kind, setKind] = useState<SetupKind>('telegram');
  return (
    <div className="channel-setup">
      <p className="muted">
        Pick how onett should reach you. Tokens are stored in <span className="mono">secrets.env</span> and are never
        shown again; the config only names the variable that holds them.
      </p>
      <div className="segmented" role="group" aria-label="Setup flow">
        {FLOWS.map((flow) => (
          <button
            key={flow.kind}
            type="button"
            className={cx(kind === flow.kind && 'on')}
            aria-pressed={kind === flow.kind}
            onClick={() => setKind(flow.kind)}
          >
            {flow.label}
          </button>
        ))}
      </div>
      {kind === 'ntfy' ? <NtfyFlow key="ntfy" doc={doc} /> : <ChatFlow key={kind} kind={kind} doc={doc} />}
    </div>
  );
}

/** Saves a token (when one was typed), then the config changes; shared by the three flows. */
function useSetupSave(doc: ConfigDocument, secretName: string) {
  const putSecret = usePutSecret();
  const save = useSaveConfigDocument();
  const [problems, setProblems] = useState<ConfigProblem[]>([]);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);

  const run = async (
    token: string,
    changes: Parameters<typeof save.mutateAsync>[0]['changes'],
    onSecretSaved: () => void,
  ): Promise<boolean> => {
    setProblems([]);
    setMessage(null);
    setError(null);
    setSaved(false);
    try {
      // The secret goes first: the config reload that follows the entry needs the token to exist.
      if (token !== '') {
        await putSecret.mutateAsync({ name: secretName, value: token });
        onSecretSaved(); // the token is stored now: never keep it in the page, even if the config save fails
      }
      await save.mutateAsync({ version: doc.version, changes });
      setSaved(true);
      return true;
    } catch (err) {
      const failure = classifySaveFailure(err);
      if (failure.kind === 'problems') setProblems(failure.problems);
      else if (failure.kind === 'conflict') setMessage('The config file changed since this page loaded. Reload the page and try again.');
      else if (failure.kind === 'written-not-reloaded') setMessage(failure.message);
      else if (err instanceof ConfigApiError) setMessage(err.message);
      else setError(err);
      return false;
    }
  };
  return { run, problems, message, error, saved, pending: putSecret.isPending || save.isPending };
}

function Problems({ problems, message, error }: { problems: ConfigProblem[]; message: string | null; error: unknown }) {
  return (
    <>
      {message ? <Callout title="Not saved">{message}</Callout> : null}
      {problems.length > 0 ? (
        <Callout title="The config has problems">
          <ul>
            {problems.map((p, i) => (
              <li key={i}>
                {p.path ? <span className="mono">{p.path}</span> : null} {p.message}
              </li>
            ))}
          </ul>
        </Callout>
      ) : null}
      <ErrorLine error={error} />
    </>
  );
}

function TokenField({ name, isSet, label, value, onChange, hint }: {
  name: string;
  isSet: boolean;
  label: string;
  value: string;
  onChange: (v: string) => void;
  hint?: string;
}) {
  const problem = value === '' ? null : valueProblem(value);
  return (
    <Field label={label} hint={problem ?? (isSet ? `${name} is already saved. Leave this empty to keep it, or paste a new one to replace it.` : hint)}>
      <input
        type="password"
        autoComplete="new-password"
        spellCheck={false}
        value={value}
        placeholder={isSet ? '•••••• (saved)' : ''}
        onChange={(event) => onChange(event.target.value)}
      />
    </Field>
  );
}

function ChatFlow({ kind, doc }: { kind: ChatKind; doc: ConfigDocument }) {
  const telegram = kind === 'telegram';
  const name = telegram ? 'Telegram' : 'Discord';
  const secretName = tokenEnvFor(doc.document, kind);
  const secrets = useSecrets();
  const tokenIsSet = secrets.data?.secrets.some((s) => s.name === secretName && s.set) ?? false;
  const entry = findEntry(doc.document, kind);
  const savedIds = existingUserIds(doc.document, kind);

  const [token, setToken] = useState('');
  const [ids, setIds] = useState(savedIds);
  const [touched, setTouched] = useState(false);
  const { run, problems, message, error, saved, pending } = useSetupSave(doc, secretName);

  // An existing entry fills the ID field once the document is known.
  useEffect(() => {
    setIds((prev) => (prev === '' ? savedIds : prev));
  }, [savedIds]);

  const parsed = parseUserIds(kind, ids);
  const idError = 'error' in parsed ? parsed.error : null;
  const needsToken = !tokenIsSet && token === '';
  const tokenError = token === '' ? null : valueProblem(token);
  const canSave = !pending && !idError && !needsToken && !tokenError;

  const onSave = async () => {
    setTouched(true);
    if (!canSave || 'error' in parsed) return;
    await run(token, chatChanges(doc.document, kind, parsed.ids), () => setToken(''));
  };

  // The test sends through the saved bot, so unsaved edits must be saved first.
  const dirty = token !== '' || ('ids' in parsed && parsed.ids.join(', ') !== savedIds);
  const testReason = !entry ? 'Save & connect first.' : dirty ? 'Save first.' : undefined;

  return (
    <section className="config-section setup-flow" aria-label={`${name} setup`}>
      <h3 className="config-section-title">{name}</h3>
      {entry ? (
        <p className="muted small">A {name} channel is already configured. Saving updates it.</p>
      ) : null}
      <ol className="setup-steps">
        {telegram ? (
          <li>
            <strong>Create a bot.</strong> In Telegram, message{' '}
            <a href="https://t.me/BotFather" target="_blank" rel="noreferrer">@BotFather</a>, send{' '}
            <span className="mono">/newbot</span> and follow the prompts. It replies with the bot token.
          </li>
        ) : (
          <li>
            <strong>Create a bot.</strong> In the{' '}
            <a href="https://discord.com/developers/applications" target="_blank" rel="noreferrer">Discord Developer Portal</a>{' '}
            choose New Application, open Bot, add a bot and choose Reset Token to reveal the token. Leave Message Content
            Intent off. Then under OAuth2 → URL Generator tick the <span className="mono">bot</span> scope, open the link it
            makes and add the bot to a server you are in (a small private server is fine): Discord only lets a bot message
            people it shares a server with.
          </li>
        )}
        <li>
          <strong>Paste the token.</strong>
          <TokenField
            name={secretName}
            isSet={tokenIsSet}
            label={`${name} bot token`}
            value={token}
            onChange={setToken}
            hint={`Saved as ${secretName}.`}
          />
          {touched && needsToken ? <p className="error-line small" role="alert">Paste the bot token.</p> : null}
        </li>
        <li>
          <strong>Enter your user ID.</strong>{' '}
          {telegram ? (
            <>Message <a href="https://t.me/userinfobot" target="_blank" rel="noreferrer">@userinfobot</a> in Telegram; it replies with your numeric ID.</>
          ) : (
            <>In Discord turn on Settings → Advanced → Developer Mode, then right-click your name and choose Copy User ID.</>
          )}{' '}
          Only the IDs listed here can talk to onett. Separate several with commas.
          <Field label={`Your ${name} user ID`} hint={touched ? idError : null}>
            <input type="text" inputMode="numeric" value={ids} onChange={(event) => setIds(event.target.value)} />
          </Field>
        </li>
        <li>
          <strong>Save &amp; connect.</strong> Writes the channel to the config and starts the bot, with no restart.
          <div>
            <Button variant="primary" disabled={pending} onClick={() => void onSave()}>
              {pending ? 'Saving…' : 'Save & connect'}
            </Button>
            {saved ? <span className="small" role="status"> Saved. {telegram ? 'Send your bot a message (/start) once so it can message you.' : 'The bot can now message you.'}</span> : null}
          </div>
          <Problems problems={problems} message={message} error={error} />
        </li>
        <li>
          <strong>Send a test message.</strong>
          {entry ? <SendTestButton kind="channels" index={entry.index} disabledReason={testReason} /> : <span className="muted small"> {testReason}</span>}
        </li>
      </ol>
    </section>
  );
}

function NtfyFlow({ doc }: { doc: ConfigDocument }) {
  const secretName = tokenEnvFor(doc.document, 'ntfy');
  const secrets = useSecrets();
  const tokenIsSet = secrets.data?.secrets.some((s) => s.name === secretName && s.set) ?? false;
  const entry = findEntry(doc.document, 'ntfy');
  const savedTopic = typeof entry?.options.topic === 'string' ? entry.options.topic : '';
  const savedServer = typeof entry?.options.server === 'string' ? entry.options.server : '';
  const hasTokenEnv = typeof entry?.options.token_env === 'string' && entry.options.token_env !== '';

  const [topic, setTopic] = useState(savedTopic || randomTopic);
  const [server, setServer] = useState(savedServer);
  const [token, setToken] = useState('');
  const [touched, setTouched] = useState(false);
  const { run, problems, message, error, saved, pending } = useSetupSave(doc, secretName);

  const topicError = topic.trim() === '' ? 'Enter a topic.' : null;
  const srvError = serverProblem(server.trim());
  const tokenError = token === '' ? null : valueProblem(token);
  const canSave = !pending && !topicError && !srvError && !tokenError;

  const onSave = async () => {
    setTouched(true);
    if (!canSave) return;
    // A token_env is written only when a token exists to back it; an ntfy notifier refuses an empty one.
    const wantsToken = token !== '' || (tokenIsSet && hasTokenEnv);
    const changes = ntfyChanges(doc.document, topic.trim(), server.trim(), wantsToken ? secretName : null);
    await run(token, changes, () => setToken(''));
  };

  const dirty = token !== '' || topic.trim() !== savedTopic || server.trim() !== savedServer;
  const testReason = !entry ? 'Save first.' : dirty ? 'Save first.' : undefined;

  return (
    <section className="config-section setup-flow" aria-label="ntfy setup">
      <h3 className="config-section-title">ntfy</h3>
      {entry ? <p className="muted small">An ntfy notifier is already configured. Saving updates it.</p> : null}
      <ol className="setup-steps">
        <li>
          <strong>Subscribe to a topic.</strong> Install the ntfy app (or open{' '}
          <a href="https://ntfy.sh" target="_blank" rel="noreferrer">ntfy.sh</a>) and subscribe to the topic below. Anyone
          who knows a topic can read it, so keep the random one suggested here.
          <Field label="Topic" hint={touched ? topicError : null}>
            <input type="text" spellCheck={false} value={topic} onChange={(event) => setTopic(event.target.value)} />
          </Field>
          <Button onClick={() => setTopic(randomTopic())}>New random topic</Button>
        </li>
        <li>
          <strong>Server.</strong> Leave blank for ntfy.sh, or enter your own server's address.
          <Field label="Server (optional)" hint={srvError}>
            <input type="text" placeholder="https://ntfy.sh" spellCheck={false} value={server} onChange={(event) => setServer(event.target.value)} />
          </Field>
        </li>
        <li>
          <strong>Access token (optional).</strong> Only for a private or protected topic.
          <TokenField
            name={secretName}
            isSet={tokenIsSet && hasTokenEnv}
            label="ntfy access token"
            value={token}
            onChange={setToken}
            hint={`Saved as ${secretName}.`}
          />
        </li>
        <li>
          <strong>Save.</strong> Writes the notifier to the config; it takes effect at once.
          <div>
            <Button variant="primary" disabled={pending} onClick={() => void onSave()}>
              {pending ? 'Saving…' : 'Save'}
            </Button>
            {saved ? <span className="small" role="status"> Saved.</span> : null}
          </div>
          <Problems problems={problems} message={message} error={error} />
        </li>
        <li>
          <strong>Send a test notification.</strong>
          {entry ? <SendTestButton kind="notify" index={entry.index} disabledReason={testReason} /> : <span className="muted small"> {testReason}</span>}
        </li>
      </ol>
    </section>
  );
}
