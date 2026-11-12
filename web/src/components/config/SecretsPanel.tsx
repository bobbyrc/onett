// The Secrets panel on the Config page (owners only). It is write-only: it shows each name, whether
// it is set, where the value comes from and what the config refers it by, but it never asks for
// or shows a value. Set and Replace take a new value in a password field that is cleared after
// the save. See docs/api.md (GET/PUT/DELETE /secrets).
import { useState, type FormEvent } from 'react';
import { useDeleteSecret, usePutSecret, useSecrets } from '../../queries';
import { SECRET_NAME_RE as NAME_RE, isShadowed, secretStatus, sortSecrets, sourceLabel, valueProblem } from '../../lib/secrets';
import type { SecretView } from '../../types';
import { Button } from '../Button';
import { Empty } from '../Empty';
import { ErrorLine } from '../ErrorLine';
import { Field } from '../Field';


export function SecretsPanel() {
  const secrets = useSecrets();
  const put = usePutSecret();
  const remove = useDeleteSecret();

  // The name whose value form is open ('' with `adding` for a brand-new name) and its input.
  const [editing, setEditing] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [newName, setNewName] = useState('');
  const [value, setValue] = useState('');
  const [notice, setNotice] = useState<string | null>(null);
  const [failure, setFailure] = useState<unknown>(null);

  const closeForm = () => {
    setEditing(null);
    setAdding(false);
    setNewName('');
    setValue('');
  };

  const save = (name: string) => {
    setFailure(null);
    setNotice(null);
    put.mutate(
      { name, value },
      {
        onSuccess: (saved) => {
          closeForm();
          setNotice(
            saved.shadowed
              ? `${saved.name} is saved, but the daemon's own environment defines it and wins. Remove it there to use the saved value.`
              : `${saved.name} is saved.`,
          );
        },
        onError: setFailure,
      },
    );
  };

  const submit = (event: FormEvent, name: string) => {
    event.preventDefault();
    save(name);
  };

  const removeSecret = (name: string) => {
    setFailure(null);
    setNotice(null);
    remove.mutate(name, {
      onSuccess: () => setNotice(`${name} is removed.`),
      onError: setFailure,
    });
  };

  const list = sortSecrets(secrets.data?.secrets ?? []);
  const nameProblem = newName !== '' && !NAME_RE.test(newName) ? 'Use capital letters, digits and underscores.' : null;
  const valueIssue = value === '' ? null : valueProblem(value);

  const valueForm = (name: string) => (
    <form className="inline-form" onSubmit={(event) => submit(event, name)}>
      <Field label={`New value for ${name}`} hint={valueIssue}>
        <input
          type="password"
          autoComplete="new-password"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          autoFocus
        />
      </Field>
      <Button type="submit" variant="primary" disabled={put.isPending || value === '' || valueIssue !== null}>
        Save
      </Button>
      <Button onClick={closeForm}>Cancel</Button>
    </form>
  );

  return (
    <section aria-label="Secrets">
      <p className="muted">
        Tokens, webhook URLs and keys live in <span className="mono">secrets.env</span>, next to the config. They can be
        set here but never read back. A saved secret applies at once, including to chat bots and notifiers.
      </p>
      <ErrorLine error={secrets.error ?? failure} />
      {notice ? (
        <p className="muted small" role="status">
          {notice}
        </p>
      ) : null}

      {secrets.isPending ? (
        <Empty>Loading secrets…</Empty>
      ) : list.length === 0 ? (
        <Empty>No secrets yet. The config does not refer to any, and secrets.env is empty.</Empty>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Source</th>
              <th>Used by</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {list.map((s: SecretView) => (
              <tr key={s.name}>
                <td className="mono">{s.name}</td>
                <td>
                  <span className={s.set ? undefined : 'muted'}>{secretStatus(s)}</span>
                  {isShadowed(s) ? <div className="muted small">shadowed by the daemon&apos;s environment</div> : null}
                </td>
                <td>{sourceLabel(s) || <span className="muted">—</span>}</td>
                <td>
                  {s.referenced_by.length === 0 ? (
                    <span className="muted">—</span>
                  ) : (
                    s.referenced_by.map((path) => (
                      <div key={path} className="mono small">
                        {path}
                      </div>
                    ))
                  )}
                </td>
                <td>
                  <div className="users-actions">
                    <button
                      type="button"
                      className="link-button small"
                      aria-expanded={editing === s.name}
                      onClick={() => {
                        setValue('');
                        setAdding(false);
                        setEditing(editing === s.name ? null : s.name);
                      }}
                    >
                      {s.set ? 'Replace' : 'Set'}
                    </button>
                    {s.source === 'file' ? (
                      <Button
                        variant="danger"
                        confirm
                        disabled={remove.isPending && remove.variables === s.name}
                        onClick={() => removeSecret(s.name)}
                      >
                        Delete
                      </Button>
                    ) : null}
                  </div>
                  {editing === s.name ? valueForm(s.name) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div>
        {adding ? (
          <form className="inline-form" onSubmit={(event) => submit(event, newName)}>
            <Field label="Name" hint={nameProblem}>
              <input
                type="text"
                className="mono"
                value={newName}
                placeholder="TELEGRAM_BOT_TOKEN"
                onChange={(event) => setNewName(event.target.value)}
                autoFocus
              />
            </Field>
            <Field label="Value" hint={valueIssue}>
              <input
                type="password"
                autoComplete="new-password"
                value={value}
                onChange={(event) => setValue(event.target.value)}
              />
            </Field>
            <Button
              type="submit"
              variant="primary"
              disabled={put.isPending || !NAME_RE.test(newName) || value === '' || valueIssue !== null}
            >
              Save
            </Button>
            <Button onClick={closeForm}>Cancel</Button>
          </form>
        ) : (
          <Button
            onClick={() => {
              setEditing(null);
              setValue('');
              setAdding(true);
            }}
          >
            Add a secret
          </Button>
        )}
      </div>
    </section>
  );
}
