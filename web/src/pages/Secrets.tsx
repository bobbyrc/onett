import { useState, type FormEvent } from 'react';
import { Button } from '../components/Button';
import { ErrorLine } from '../components/ErrorLine';
import { Field } from '../components/Field';
import { PageHeader } from '../components/PageHeader';
import { Table, type Column } from '../components/Table';
import { useDeleteSecret, useSecrets, useSetSecret } from '../queries';

/** Owner only. Names in secrets.env; values are write-only and never shown. */
export function Secrets() {
  const secrets = useSecrets();
  const save = useSetSecret();
  const remove = useDeleteSecret();

  const [name, setName] = useState('');
  const [value, setValue] = useState('');
  const [failure, setFailure] = useState<unknown>(null);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setFailure(null);
    save.mutate(
      { name: name.trim(), value },
      {
        onSuccess: () => {
          setName('');
          setValue('');
        },
      },
    );
  };

  const columns: Column<string>[] = [
    { key: 'name', header: 'Name', cell: (n) => <span className="mono">{n}</span> },
    {
      key: 'actions',
      header: 'Actions',
      cell: (n) => (
        <div className="row-actions">
          <button
            type="button"
            className="link-button small"
            onClick={() => {
              setName(n);
              setValue('');
            }}
          >
            Replace
          </button>
          <Button
            variant="danger"
            confirm
            disabled={remove.isPending && remove.variables === n}
            onClick={() => {
              setFailure(null);
              remove.mutate(n, { onError: setFailure });
            }}
          >
            Remove
          </Button>
        </div>
      ),
    },
  ];

  return (
    <>
      <PageHeader title="Secrets" />
      <p className="muted">
        Environment variables kept in <span className="mono">{secrets.data?.path ?? 'secrets.env'}</span>, such as
        bot tokens and API keys. A config entry like <span className="mono">bot_token_env</span> names one of them.
        Values are never shown again after you save them. A saved value applies to the next config reload and the
        next agent run; a removal, and a value already set in the daemon&apos;s own environment, need a restart.
      </p>

      <form className="card" onSubmit={submit}>
        <div className="users-form-row">
          <Field label="Name" hint="Letters, digits and _, for example TELEGRAM_BOT_TOKEN">
            <input
              type="text"
              name="secret-name"
              autoComplete="off"
              spellCheck={false}
              required
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          <Field label="Value">
            <input
              type="password"
              name="secret-value"
              autoComplete="new-password"
              required
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          </Field>
        </div>
        <ErrorLine error={save.error} />
        <div className="row-actions">
          <Button type="submit" variant="primary" disabled={save.isPending}>
            Save secret
          </Button>
        </div>
      </form>

      <ErrorLine error={secrets.error} />
      {secrets.isSuccess ? (
        <div className="users-table">
          <Table columns={columns} rows={secrets.data.names} rowKey={(n) => n} empty="No secrets yet." />
        </div>
      ) : null}
      <ErrorLine error={failure} />
    </>
  );
}
