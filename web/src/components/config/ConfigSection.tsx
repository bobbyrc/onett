// Renders one node of the config schema by its type: sections, maps, lists, scalars and `any`.
// Every edit goes through the EditContext, which writes to the change list. Nothing here holds
// the file's values itself, so the page always shows what would be saved.
import { useId, useState } from 'react';
import type { SchemaNode } from '../../types';
import {
  activeVariant,
  dotted,
  emptyValue,
  envRefName,
  getAt,
  hasProblemUnder,
  humanizeKey,
  singularize,
  splitOptionsForVariant,
  type Path,
} from '../../lib/configDoc';
import { cx } from '../../lib/format';
import { AnyInput, ScalarInput, isScalarType } from './ConfigFieldInput';
import { SecretPicker } from './SecretPicker';

export interface EditContext {
  /** The file with the pending changes applied: what the inputs show. */
  draft: unknown;
  /** Problems from the last refused save, keyed by dotted path. */
  problems: Map<string, string[]>;
  /** Sets a value. Used for scalars, whole lists, new entries and new list elements. */
  setValue: (path: Path, value: unknown) => void;
  /** Clears a value: removes its key, or drops the earlier edit when the file never had it. */
  clearValue: (path: Path) => void;
  /** A top-level map entry to bring into view (?entry=onett), so a link can open one project. */
  focusEntry?: string;
}

/** Messages for one path, from the last refused save. */
function issuesAt(ctx: EditContext, path: Path): string[] {
  return ctx.problems.get(dotted(path)) ?? [];
}

function Issues({ messages }: { messages: string[] }) {
  return (
    <>
      {messages.map((message, index) => (
        <p key={index} className="error-line">
          {message}
        </p>
      ))}
    </>
  );
}

/** A node with its type-specific editor. The page and every nested block enter through here. */
export function NodeEditor({
  node,
  path,
  ctx,
  nested,
  variantSibling,
}: {
  node: SchemaNode;
  path: Path;
  ctx: EditContext;
  nested: boolean;
  /** Set on the discriminator field (`type`): the variant node its value selects. */
  variantSibling?: SchemaNode;
}) {
  switch (node.type) {
    case 'section':
      return <SectionBlock node={node} path={path} ctx={ctx} nested={nested} />;
    case 'map':
      return <MapEditor node={node} path={path} ctx={ctx} />;
    case 'list':
      return <ListEditor node={node} path={path} ctx={ctx} />;
    case 'any':
      return <AnyEditor node={node} path={path} ctx={ctx} />;
    case 'variant':
      return <VariantEditor node={node} path={path} ctx={ctx} />;
    default:
      return <FieldRow node={node} path={path} ctx={ctx} variantSibling={variantSibling} />;
  }
}

/** Fields of a section or of a map entry, in schema order, indented one step. */
function FieldList({
  fields,
  path,
  ctx,
}: {
  fields: SchemaNode[];
  path: Path;
  ctx: EditContext;
}) {
  const variant = fields.find((f) => f.type === 'variant');
  return (
    <div className="config-indent">
      {fields.map((field) => (
        <NodeEditor
          key={field.key}
          node={field}
          path={[...path, field.key]}
          ctx={ctx}
          nested={true}
          variantSibling={variant && field.key === variant.discriminator ? variant : undefined}
        />
      ))}
    </div>
  );
}

/**
 * An `options` map whose fields depend on the sibling `type`. With a known type it shows that
 * type's fields; with none, or one this onett does not know, it falls back to raw JSON.
 */
function VariantEditor({ node, path, ctx }: { node: SchemaNode; path: Path; ctx: EditContext }) {
  const active = activeVariant(node, ctx.draft, path.slice(0, -1));
  if (!active) return <AnyEditor node={node} path={path} ctx={ctx} />;
  if ((active.fields ?? []).length === 0) {
    return (
      <section className="config-section">
        <BlockHead node={node} path={path} ctx={ctx} label={humanizeKey(node.key)} />
        <p className="muted">This type has no options.</p>
      </section>
    );
  }
  return (
    <section className="config-section">
      <BlockHead node={node} path={path} ctx={ctx} label={humanizeKey(node.key)} />
      <FieldList fields={active.fields ?? []} path={path} ctx={ctx} />
    </section>
  );
}

/** A heading with its help text and any problems on the block itself. */
function BlockHead({
  node,
  path,
  ctx,
  label,
}: {
  node: SchemaNode;
  path: Path;
  ctx: EditContext;
  label: string;
}) {
  return (
    <>
      <h3 className="config-section-title">{label}</h3>
      {node.help ? <p className="config-field-help">{node.help}</p> : null}
      <Issues messages={issuesAt(ctx, path)} />
    </>
  );
}

function SectionBlock({
  node,
  path,
  ctx,
  nested,
}: {
  node: SchemaNode;
  path: Path;
  ctx: EditContext;
  nested: boolean;
}) {
  const fields = node.fields ?? [];
  return (
    <section className="config-section">
      <BlockHead node={node} path={path} ctx={ctx} label={humanizeKey(node.key)} />
      {nested ? (
        <FieldList fields={fields} path={path} ctx={ctx} />
      ) : (
        fields.map((field) => (
          <NodeEditor key={field.key} node={field} path={[...path, field.key]} ctx={ctx} nested={true} />
        ))
      )}
    </section>
  );
}

/** One scalar: label, dotted path, help, the input, then the environment note and problems. */
function FieldRow({
  node,
  path,
  ctx,
  variantSibling,
}: {
  node: SchemaNode;
  path: Path;
  ctx: EditContext;
  variantSibling?: SchemaNode;
}) {
  const id = useId();
  // A pending switch of `type` that would drop options, waiting for a yes or no.
  const [pending, setPending] = useState<{ next: string; dropped: string[] } | null>(null);
  const labelId = `${id}-label`;
  const value = getAt(ctx.draft, path);
  const issues = issuesAt(ctx, path);
  const envName = envRefName(value);
  const optionsPath = [...path.slice(0, -1), 'options'];
  const apply = (next: unknown) => {
    if (next === undefined) ctx.clearValue(path);
    else ctx.setValue(path, next);
  };
  const switchVariant = (next: string | undefined, dropped: string[], kept: Record<string, unknown>) => {
    apply(next);
    if (dropped.length > 0) {
      if (Object.keys(kept).length === 0) ctx.clearValue(optionsPath);
      else ctx.setValue(optionsPath, kept);
    }
  };
  const onChange = (next: unknown) => {
    if (!variantSibling || (next !== undefined && typeof next !== 'string')) {
      apply(next);
      return;
    }
    const target = next === undefined ? undefined : variantSibling.variants?.[next];
    const { kept, dropped } = splitOptionsForVariant(getAt(ctx.draft, optionsPath), target);
    if (dropped.length === 0) switchVariant(next, dropped, kept);
    else setPending({ next: next ?? '', dropped });
  };
  return (
    <div className="config-field">
      <div className="config-field-head">
        {node.type === 'bool' ? (
          <span id={labelId} className="config-field-label">
            {humanizeKey(node.key)}
          </span>
        ) : (
          <label htmlFor={id} className="config-field-label">
            {humanizeKey(node.key)}
          </label>
        )}
        <span className="config-field-path mono">{dotted(path)}</span>
      </div>
      {node.help ? <p className="config-field-help">{node.help}</p> : null}
      <div className="config-field-input">
        {node.secret_env && node.type === 'string' ? (
          <SecretPicker
            id={id}
            fieldKey={node.key}
            help={node.help}
            type={getAt(ctx.draft, [...path.slice(0, -2), 'type'])}
            value={value}
            invalid={issues.length > 0}
            onChange={onChange}
          />
        ) : (
          <ScalarInput
            id={id}
            labelledBy={node.type === 'bool' ? labelId : undefined}
            type={node.type}
            value={value}
            invalid={issues.length > 0}
            enumValues={node.enum}
            onChange={onChange}
          />
        )}
      </div>
      {pending ? (
        <p className="config-env" role="alert">
          Changing to {pending.next || '(not set)'} drops the options it does not use:{' '}
          <span className="mono">{pending.dropped.join(', ')}</span>.{' '}
          <button
            type="button"
            className="link-button"
            onClick={() => {
              const target = pending.next === '' ? undefined : variantSibling?.variants?.[pending.next];
              const { kept, dropped } = splitOptionsForVariant(getAt(ctx.draft, optionsPath), target);
              switchVariant(pending.next === '' ? undefined : pending.next, dropped, kept);
              setPending(null);
            }}
          >
            Change
          </button>{' '}
          <button type="button" className="link-button" onClick={() => setPending(null)}>
            Keep {typeof value === 'string' && value !== '' ? value : 'current'}
          </button>
        </p>
      ) : null}
      {node.secret_env ? (
        <p className="muted config-env">
          This is the name of an environment variable that holds the secret, not the secret itself.
        </p>
      ) : null}
      {envName ? (
        <p className="muted config-env">
          Read from the environment variable {envName} when onett loads the file.
        </p>
      ) : null}
      <Issues messages={issues} />
    </div>
  );
}

function AnyEditor({ node, path, ctx }: { node: SchemaNode; path: Path; ctx: EditContext }) {
  const id = useId();
  const value = getAt(ctx.draft, path);
  const issues = issuesAt(ctx, path);
  return (
    <div className="config-field">
      <div className="config-field-head">
        <label htmlFor={id} className="config-field-label">
          {humanizeKey(node.key)} (JSON)
        </label>
        <span className="config-field-path mono">{dotted(path)}</span>
      </div>
      {node.help ? <p className="config-field-help">{node.help}</p> : null}
      <AnyInput
        id={id}
        value={value}
        invalid={issues.length > 0}
        onChange={(next) => {
          if (next === undefined) ctx.clearValue(path);
          else ctx.setValue(path, next);
        }}
      />
      <Issues messages={issues} />
    </div>
  );
}

/** The add-a-name form for maps. It checks the name before it adds anything. */
function AddEntry({
  singular,
  taken,
  onAdd,
}: {
  singular: string;
  taken: string[];
  onAdd: (name: string) => void;
}) {
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  return (
    <form
      className="config-add"
      onSubmit={(event) => {
        event.preventDefault();
        const trimmed = name.trim();
        if (trimmed === '') {
          setError('Enter a name.');
          return;
        }
        if (taken.includes(trimmed)) {
          setError(`A ${singular} named ${trimmed} already exists.`);
          return;
        }
        setError(null);
        setName('');
        onAdd(trimmed);
      }}
    >
      <input
        className="config-input"
        aria-label={`New ${singular} name`}
        placeholder={`${singular} name`}
        value={name}
        onChange={(event) => {
          setName(event.target.value);
          setError(null);
        }}
      />
      <button type="submit" className="btn btn-secondary">
        Add {singular}
      </button>
      {error ? <p className="error-line config-add-error">{error}</p> : null}
    </form>
  );
}

function scrollIntoView(el: HTMLElement | null) {
  el?.scrollIntoView?.({ block: 'start' });
}

/** The one inline question open on a map entry, so only one entry asks at a time. */
type EntryAction = { op: 'remove' | 'rename'; name: string } | null;

function MapEditor({ node, path, ctx }: { node: SchemaNode; path: Path; ctx: EditContext }) {
  const value = getAt(ctx.draft, path);
  const names =
    value !== null && typeof value === 'object' && !Array.isArray(value) ? Object.keys(value) : [];
  const item: SchemaNode = node.item ?? { key: node.key, type: 'any' };
  const singular = singularize(node.key).replace(/_/g, ' ');
  const [action, setAction] = useState<EntryAction>(null);
  const [renameTo, setRenameTo] = useState('');
  const [renameError, setRenameError] = useState<string | null>(null);
  // Only a top-level map (path of one key) takes the link's entry; nested maps ignore it.
  const focused =
    path.length === 1 && ctx.focusEntry && names.includes(ctx.focusEntry) ? ctx.focusEntry : undefined;

  const add = (name: string) => ctx.setValue([...path, name], emptyValue(item));

  const renameEntry = (oldName: string) => {
    const next = renameTo.trim();
    if (next === '') {
      setRenameError('Enter a name.');
      return;
    }
    if (next === oldName) {
      setAction(null);
      return;
    }
    if (names.includes(next)) {
      setRenameError(`A ${singular} named ${next} already exists.`);
      return;
    }
    // A rename is the old key removed and the new key set to the same value.
    const old = getAt(ctx.draft, [...path, oldName]);
    ctx.clearValue([...path, oldName]);
    ctx.setValue([...path, next], old);
    setAction(null);
    setRenameError(null);
  };

  const head = <BlockHead node={node} path={path} ctx={ctx} label={humanizeKey(node.key)} />;

  if (isScalarType(item.type)) {
    const values = (value ?? {}) as Record<string, unknown>;
    return (
      <section className="config-section">
        {head}
        <table className="table config-map-table">
          <thead>
            <tr>
              <th scope="col">{humanizeKey(singular)}</th>
              <th scope="col">Value</th>
              <th scope="col">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {names.map((name) => {
              const entryPath = [...path, name];
              const issues = issuesAt(ctx, entryPath);
              return (
                <tr key={name}>
                  <td className="mono">{name}</td>
                  <td>
                    <ScalarInput
                      id={dotted(entryPath)}
                      label={`${humanizeKey(singular)} ${name}`}
                      type={item.type}
                      value={values[name]}
                      invalid={issues.length > 0}
                      onChange={(next) => {
                        if (next === undefined) ctx.clearValue(entryPath);
                        else ctx.setValue(entryPath, next);
                      }}
                    />
                    <Issues messages={issues} />
                  </td>
                  <td>
                    <button type="button" className="link-button" onClick={() => ctx.clearValue(entryPath)}>
                      Remove
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        <AddEntry singular={singular} taken={names} onAdd={add} />
      </section>
    );
  }

  return (
    <section className="config-section">
      {head}
      {names.map((name) => {
        const entryPath = [...path, name];
        const open = action?.name === name ? action.op : null;
        return (
          <div
            className={cx('card', 'config-entry', focused === name && 'config-entry-focus')}
            key={name}
            id={focused === name ? `config-entry-${name}` : undefined}
            ref={focused === name ? scrollIntoView : undefined}
          >
            <div className="config-entry-head">
              <span className="config-entry-name">{name}</span>
              <span className="row-actions">
                {open === 'remove' ? (
                  <>
                    <span>Remove {name}?</span>
                    <button
                      type="button"
                      className="link-button"
                      onClick={() => {
                        ctx.clearValue(entryPath);
                        setAction(null);
                      }}
                    >
                      Yes
                    </button>
                    <button type="button" className="link-button" onClick={() => setAction(null)}>
                      No
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      type="button"
                      className="link-button"
                      onClick={() => {
                        setAction({ op: 'rename', name });
                        setRenameTo(name);
                        setRenameError(null);
                      }}
                    >
                      Rename
                    </button>
                    <button
                      type="button"
                      className="link-button"
                      onClick={() => setAction({ op: 'remove', name })}
                    >
                      Remove
                    </button>
                  </>
                )}
              </span>
            </div>
            {open === 'rename' ? (
              <form
                className="config-rename"
                onSubmit={(event) => {
                  event.preventDefault();
                  renameEntry(name);
                }}
              >
                <input
                  className="config-input"
                  aria-label={`New name for ${name}`}
                  value={renameTo}
                  onChange={(event) => setRenameTo(event.target.value)}
                />
                <button type="submit" className="btn btn-secondary">
                  Rename
                </button>
                <button type="button" className="btn btn-ghost" onClick={() => setAction(null)}>
                  Cancel
                </button>
                {renameError ? <p className="error-line">{renameError}</p> : null}
              </form>
            ) : null}
            <Issues messages={issuesAt(ctx, entryPath)} />
            {item.type === 'section' ? (
              <FieldList fields={item.fields ?? []} path={entryPath} ctx={ctx} />
            ) : (
              <NodeEditor node={{ ...item, key: name }} path={entryPath} ctx={ctx} nested={true} />
            )}
          </div>
        );
      })}
      <AddEntry singular={singular} taken={names} onAdd={add} />
    </section>
  );
}

function ListEditor({ node, path, ctx }: { node: SchemaNode; path: Path; ctx: EditContext }) {
  const value = getAt(ctx.draft, path);
  const items = Array.isArray(value) ? value : [];
  const item: SchemaNode = node.item ?? { key: node.key, type: 'any' };
  const singular = singularize(node.key).replace(/_/g, ' ');
  const scalar = isScalarType(item.type);

  const add = () => {
    // An existing list gets the new element at its end; a missing list is created with it.
    if (Array.isArray(value)) ctx.setValue([...path, String(items.length)], emptyValue(item));
    else ctx.setValue(path, [emptyValue(item)]);
  };

  // Removing shifts the later indexes, so the whole list is set again rather than deleting one
  // index. That keeps the replayed changes matching what the owner sees.
  const removeAt = (index: number) => ctx.setValue(path, items.filter((_, i) => i !== index));

  return (
    <section className="config-section">
      <BlockHead node={node} path={path} ctx={ctx} label={humanizeKey(node.key)} />
      {items.map((element, index) => {
        const elementPath = [...path, String(index)];
        const issues = issuesAt(ctx, elementPath);
        if (scalar) {
          return (
            <div className="config-list-row" key={index}>
              <div className="config-list-input">
                <ScalarInput
                  id={dotted(elementPath)}
                  label={`${humanizeKey(singular)} ${index + 1}`}
                  type={item.type}
                  value={element}
                  invalid={issues.length > 0}
                  onChange={(next) => {
                    if (next === undefined) ctx.clearValue(elementPath);
                    else ctx.setValue(elementPath, next);
                  }}
                />
                <Issues messages={issues} />
              </div>
              <button type="button" className="link-button" onClick={() => removeAt(index)}>
                Remove
              </button>
            </div>
          );
        }
        return (
          <div className="card config-entry" key={index}>
            <div className="config-entry-head">
              <span className="config-entry-name">
                {humanizeKey(singular)} {index + 1}
              </span>
              <span className="row-actions">
                <button type="button" className="link-button" onClick={() => removeAt(index)}>
                  Remove
                </button>
              </span>
            </div>
            <Issues messages={issues} />
            {item.type === 'section' ? (
              <FieldList fields={item.fields ?? []} path={elementPath} ctx={ctx} />
            ) : (
              <NodeEditor node={{ ...item, key: singular }} path={elementPath} ctx={ctx} nested={true} />
            )}
          </div>
        );
      })}
      <button type="button" className="btn btn-secondary config-list-add" onClick={add}>
        Add {singular}
      </button>
    </section>
  );
}

/** True when a problem sits in the section (or under it). Marks the section in the list on the left. */
export function sectionHasProblems(problemPaths: Iterable<string>, key: string): boolean {
  return hasProblemUnder(problemPaths, [key]);
}
