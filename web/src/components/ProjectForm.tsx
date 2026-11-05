import { useState, type FormEvent } from 'react';
import { ConfigApiError } from '../api';
import { emptyProjectForm, projectFormError, type ProjectFormValues } from '../lib/projectForm';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { Field } from './Field';

/**
 * The Add project form. The caller sends the values and passes back the pending flag and the
 * error; a 422 from the server shows its problems as a list under the form.
 */
export function ProjectForm({
  policies,
  existing,
  pending,
  error,
  onSubmit,
  onCancel,
}: {
  policies: string[];
  existing: string[];
  pending: boolean;
  error: unknown;
  onSubmit: (values: ProjectFormValues) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<ProjectFormValues>(emptyProjectForm);
  const [formError, setFormError] = useState<string | null>(null);
  const set = (key: keyof ProjectFormValues) => (value: string) => setValues((v) => ({ ...v, [key]: value }));

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const problem = projectFormError(values, existing);
    setFormError(problem);
    if (!problem) onSubmit(values);
  };

  const problems = error instanceof ConfigApiError ? error.problems : [];

  return (
    <form className="form-stack" onSubmit={submit}>
      <Field label="Name" hint="Letters, digits, '.', '_' and '-'.">
        <input type="text" value={values.name} onChange={(e) => set('name')(e.target.value)} />
      </Field>
      <Field label="Repository" hint="GitHub owner/name.">
        <input type="text" value={values.repo} onChange={(e) => set('repo')(e.target.value)} />
      </Field>
      <Field
        label="Local path"
        hint="Where the clone lives. Left empty it is <data_dir>/repos/<name>. Onett does not clone for you: the repository must be there."
      >
        <input type="text" value={values.path} onChange={(e) => set('path')(e.target.value)} />
      </Field>
      <Field label="Default branch" hint="Left empty it is main.">
        <input type="text" value={values.branch} onChange={(e) => set('branch')(e.target.value)} />
      </Field>
      <Field label="Policy" hint="Decides what needs your approval.">
        <select value={values.policy} onChange={(e) => set('policy')(e.target.value)}>
          <option value="">Default</option>
          {policies.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      </Field>
      <Field label="Instructions" hint="Added to every agent prompt for this project.">
        <textarea rows={4} value={values.instructions} onChange={(e) => set('instructions')(e.target.value)} />
      </Field>
      {formError ? (
        <p className="error-line" role="alert">
          {formError}
        </p>
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
        <ErrorLine error={error} />
      )}
      <div className="row-actions">
        <Button type="submit" variant="primary" disabled={pending}>
          Add project
        </Button>
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
