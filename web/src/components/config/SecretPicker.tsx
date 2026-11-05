// The input for a `secret_env` field: a setting that holds the NAME of an environment variable
// that holds a secret. It offers the names the daemon knows (with set / not set), accepts a new
// name, and can store the value for the chosen name right here. Saving the value is separate from
// the config draft: the name still goes through the Save bar. It never reads or shows a value.
import { useState } from 'react';
import { usePutSecret, useSecrets } from '../../queries';
import { SECRET_NAME_RE, isShadowed, secretStatus, sortSecrets, valueProblem } from '../../lib/secrets';
import { Button } from '../Button';
import { ErrorLine } from '../ErrorLine';

const NAME_RE = SECRET_NAME_RE;
// A name the help text spells out, such as "Default TELEGRAM_BOT_TOKEN". It needs an underscore,
// so a word in capitals (ONETT, URL) is not taken for one.
const HELP_NAME_RE = /\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b/;

/** Why a name cannot be used, or null. Empty is fine: the field is then simply unset. */
export function secretNameProblem(name: string): string | null {
  if (name === '' || NAME_RE.test(name)) return null;
  return 'Use capital letters, digits and underscores, and do not start with a digit.';
}

/**
 * The name to suggest for an empty field. A name the schema help gives wins; otherwise it is built
 * from the sibling `type` and the key: telegram + bot_token_env is TELEGRAM_BOT_TOKEN.
 */
export function suggestSecretName(key: string, help: string | undefined, type: unknown): string {
  const fromHelp = help?.match(HELP_NAME_RE)?.[0];
  if (fromHelp) return fromHelp;
  const base = key.replace(/_env$/, '').toUpperCase();
  const prefix = typeof type === 'string' ? type.toUpperCase().replace(/[^A-Z0-9]+/g, '_') : '';
  const name = prefix && !base.startsWith(prefix) ? `${prefix}_${base}` : base;
  return NAME_RE.test(name) ? name : '';
}

interface SecretPickerProps {
  id: string;
  /** The field's key, such as bot_token_env. */
  fieldKey: string;
  help?: string;
  /** The sibling `type` (telegram, discord, ...), when the field sits in a variant's options. */
  type?: unknown;
  value: unknown;
  invalid?: boolean;
  onChange: (next: unknown) => void;
}

export function SecretPicker({ id, fieldKey, help, type, value, invalid, onChange }: SecretPickerProps) {
  const secrets = useSecrets();
  const put = usePutSecret();
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState('');
  const [notice, setNotice] = useState<string | null>(null);
  const [failure, setFailure] = useState<unknown>(null);

  const name = typeof value === 'string' ? value : '';
  const known = sortSecrets(secrets.data?.secrets ?? []);
  const current = known.find((s) => s.name === name);
  const problem = secretNameProblem(name);
  const usable = name !== '' && problem === null;
  const suggestion = name === '' ? suggestSecretName(fieldKey, help, type) : '';
  const valueIssue = secret === '' ? null : valueProblem(secret);

  const close = () => {
    setOpen(false);
    setSecret('');
  };

  const save = () => {
    setFailure(null);
    setNotice(null);
    put.mutate(
      { name, value: secret },
      {
        onSuccess: (saved) => {
          close();
          setNotice(
            saved.shadowed
              ? `Secret saved, but the daemon's own environment defines ${saved.name} and wins (shadowed by the daemon's environment). Save the config to use the name.`
              : 'Secret saved. Save the config to use it.',
          );
        },
        onError: setFailure,
      },
    );
  };

  return (
    <div className="secret-picker">
      <input
        id={id}
        className="config-input mono"
        type="text"
        list={`${id}-names`}
        autoComplete="off"
        spellCheck={false}
        aria-invalid={invalid || problem !== null || undefined}
        placeholder={suggestion || undefined}
        value={name}
        onChange={(event) => {
          // A different name is a different secret: forget the form and the last result.
          close();
          setNotice(null);
          setFailure(null);
          const raw = event.target.value.trim();
          onChange(raw === '' ? undefined : raw);
        }}
      />
      <datalist id={`${id}-names`}>
        {known.map((s) => (
          <option key={s.name} value={s.name}>
            {secretStatus(s)}
          </option>
        ))}
      </datalist>

      {problem ? <p className="error-line">{problem}</p> : null}

      {suggestion ? (
        <p className="muted config-env">
          Suggested name: <span className="mono">{suggestion}</span>{' '}
          <button type="button" className="link-button" onClick={() => onChange(suggestion)}>
            Use it
          </button>
        </p>
      ) : null}

      {usable ? (
        <p className="muted config-env">
          <span className="mono">{name}</span>:{' '}
          <span data-testid="secret-status">{secrets.isPending ? 'checking…' : current?.set ? 'set' : 'not set'}</span>
          {current && isShadowed(current) ? ' (shadowed by the daemon’s environment)' : null}{' '}
          <button
            type="button"
            className="link-button"
            aria-expanded={open}
            onClick={() => {
              setSecret('');
              setOpen(!open);
            }}
          >
            {current?.set ? 'Replace' : 'Set value'}
          </button>
        </p>
      ) : null}

      {usable && open ? (
        <div className="inline-form">
          <label className="field">
            <span className="field-label">{`Value for ${name}`}</span>
            <input
              type="password"
              autoComplete="new-password"
              value={secret}
              onChange={(event) => setSecret(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && secret !== '' && valueIssue === null && !put.isPending) {
                  event.preventDefault();
                  save();
                }
              }}
              autoFocus
            />
            {valueIssue ? <span className="field-hint">{valueIssue}</span> : null}
          </label>
          <p className="muted config-env">
            Stored in secrets.env and never shown again. This does not change the config: save the config afterwards.
          </p>
          <div>
            <Button variant="primary" disabled={put.isPending || secret === '' || valueIssue !== null} onClick={save}>
              Save
            </Button>{' '}
            <Button onClick={close}>Cancel</Button>
          </div>
        </div>
      ) : null}

      {notice ? (
        <p className="muted config-env" role="status">
          {notice}
        </p>
      ) : null}
      <ErrorLine error={failure} />
    </div>
  );
}
