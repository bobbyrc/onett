// The input for a `*_env` setting: pick one of the secrets onett knows, or create one right here.
// Creating writes the value to secrets.env (write-only, like the Secrets tab) and then points the
// setting at the new name. The value is never kept after the save.
import { useState, type FormEvent } from 'react';
import { usePutSecret, useSecrets } from '../../queries';
import { SECRET_NAME_RE, pickerNames, valueProblem } from '../../lib/secrets';
import { Button } from '../Button';
import { ErrorLine } from '../ErrorLine';

const NEW = '\u0000new';

export function SecretPicker({
  id,
  invalid,
  value,
  onChange,
}: {
  id: string;
  invalid?: boolean;
  value: unknown;
  onChange: (next: unknown) => void;
}) {
  const secrets = useSecrets();
  const put = usePutSecret();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [secret, setSecret] = useState('');
  const [failure, setFailure] = useState<unknown>(null);

  const current = typeof value === 'string' ? value : '';
  const nameProblem = name !== '' && !SECRET_NAME_RE.test(name) ? 'Use capital letters, digits and underscores.' : null;
  const valueIssue = secret === '' ? null : valueProblem(secret);

  const close = () => {
    setCreating(false);
    setName('');
    setSecret('');
    setFailure(null);
  };

  const create = (event: FormEvent) => {
    event.preventDefault();
    setFailure(null);
    put.mutate(
      { name, value: secret },
      {
        onSuccess: (saved) => {
          onChange(saved.name);
          close();
        },
        onError: setFailure,
      },
    );
  };

  return (
    <>
      <select
        id={id}
        className="config-input"
        aria-invalid={invalid || undefined}
        value={creating ? NEW : current}
        onChange={(event) => {
          if (event.target.value === NEW) setCreating(true);
          else {
            close();
            onChange(event.target.value === '' ? undefined : event.target.value);
          }
        }}
      >
        <option value="">(not set)</option>
        {pickerNames(secrets.data?.secrets ?? [], current).map((n) => (
          <option key={n} value={n}>
            {n}
          </option>
        ))}
        <option value={NEW}>Create a new secret…</option>
      </select>
      {creating ? (
        <form className="inline-form" onSubmit={create}>
          <label>
            Name
            <input
              type="text"
              className="mono"
              value={name}
              placeholder="TELEGRAM_BOT_TOKEN"
              autoComplete="off"
              spellCheck={false}
              onChange={(event) => setName(event.target.value)}
              autoFocus
            />
          </label>
          {nameProblem ? <p className="error-line">{nameProblem}</p> : null}
          <label>
            Value
            <input
              type="password"
              autoComplete="new-password"
              value={secret}
              onChange={(event) => setSecret(event.target.value)}
            />
          </label>
          {valueIssue ? <p className="error-line">{valueIssue}</p> : null}
          <Button
            type="submit"
            variant="primary"
            disabled={put.isPending || !SECRET_NAME_RE.test(name) || secret === '' || valueIssue !== null}
          >
            Save secret
          </Button>
          <Button onClick={close}>Cancel</Button>
          <ErrorLine error={failure} />
        </form>
      ) : null}
    </>
  );
}
