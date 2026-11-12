import { useId, useState, type ChangeEvent, type FormEvent } from 'react';
import '../pages/Triggers.css';
import type { Trigger } from '../types';
import { cx } from '../lib/format';
import {
  createBody,
  firesSentence,
  formProblem,
  initialValues,
  patchBody,
  scheduleProblem,
  timeZoneChoices,
  type TriggerFormValues,
  type TriggerStarter,
} from '../lib/triggerForm';
import {
  DAY_NAMES,
  HOUR_STEPS,
  MINUTE_STEPS,
  presetToCron,
  type Repeat,
  type SchedulePreset,
} from '../lib/triggerSchedule';
import { useProjects } from '../queries';
import { useCreateTrigger, usePatchTrigger } from '../triggerQueries';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { Field } from './Field';
import { Modal } from './Modal';

const REPEATS: [Repeat, string][] = [
  ['daily', 'Every day'],
  ['days', 'On chosen days'],
  ['hours', 'Every few hours'],
  ['minutes', 'Every few minutes'],
  ['custom', 'Custom'],
];

/**
 * Creates a trigger, or edits one. A new trigger is sent with `enabled` set, because the API's own
 * default is off. An edit sends only what changed, because sending `enabled: true` again would
 * re-arm an issue trigger.
 */
export function TriggerDialog({ trigger, starter, onClose }: { trigger?: Trigger; starter?: TriggerStarter; onClose: () => void }) {
  const projects = useProjects();
  const create = useCreateTrigger();
  const patchTrigger = usePatchTrigger();
  const startsId = useId();
  const daysId = useId();
  const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone ?? '';
  const [values, setValues] = useState<TriggerFormValues>(() => initialValues(trigger, starter, browserZone, ''));
  const [problem, setProblem] = useState<string | null>(null);
  // The trigger as it was when the dialog opened. An edit sends what this dialog changed, not
  // what changed elsewhere meanwhile: sending "on" again would re-arm an issue trigger.
  const [original] = useState(trigger);

  const projectNames = (projects.data ?? []).map((p) => p.name);
  const project = trigger ? trigger.project : values.project || projectNames[0] || '';
  const repo = (projects.data ?? []).find((p) => p.name === project)?.repo || "the project's repository";
  const { preset } = values;
  const cron = presetToCron(preset);
  const label = values.label.trim();

  const setPreset = (change: Partial<SchedulePreset>) => setValues((v) => ({ ...v, preset: { ...v.preset, ...change } }));

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const toSend = { ...values, project };
    const next = formProblem(toSend);
    setProblem(next);
    if (next) return;
    if (!original) {
      create.mutate(createBody(toSend), { onSuccess: onClose });
      return;
    }
    const patch = patchBody(original, toSend);
    if (Object.keys(patch).length === 0) {
      onClose();
      return;
    }
    patchTrigger.mutate({ id: original.id, patch }, { onSuccess: onClose });
  };

  return (
    <Modal open title={trigger ? 'Edit trigger' : 'New trigger'} onClose={onClose}>
      <form className="form-stack" onSubmit={submit} noValidate>
        {trigger ? (
          <>
            <Field label="Name">
              <input type="text" value={values.name} onChange={(e) => setValues((v) => ({ ...v, name: e.target.value }))} />
            </Field>
            <p className="muted small trg-fixed">
              {trigger.kind === 'schedule' ? 'Schedule' : 'GitHub issue'} trigger for {trigger.project}. The kind and the project
              cannot be changed; make a new trigger for another one.
            </p>
          </>
        ) : (
          <>
            <div className="trg-row">
              <Field label="Name">
                <input type="text" autoFocus value={values.name} onChange={(e) => setValues((v) => ({ ...v, name: e.target.value }))} />
              </Field>
              <Field label="Project">
                <select value={project} onChange={(e) => setValues((v) => ({ ...v, project: e.target.value }))}>
                  {projectNames.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
            <div className="field">
              <span className="field-label" id={startsId}>
                Starts
              </span>
              <div className="segmented trg-kind" role="group" aria-labelledby={startsId}>
                <button
                  type="button"
                  className={values.kind === 'schedule' ? 'on' : undefined}
                  aria-pressed={values.kind === 'schedule'}
                  onClick={() => setValues((v) => ({ ...v, kind: 'schedule' }))}
                >
                  On a schedule
                </button>
                <button
                  type="button"
                  className={values.kind === 'github_issue' ? 'on' : undefined}
                  aria-pressed={values.kind === 'github_issue'}
                  onClick={() => setValues((v) => ({ ...v, kind: 'github_issue' }))}
                >
                  On a new issue
                </button>
              </div>
              <span className="field-hint">The kind and the project cannot be changed after the trigger is created.</span>
            </div>
          </>
        )}

        {values.kind === 'schedule' ? (
          <>
            <div className="trg-row">
              <Field label="Repeats">
                <select
                  value={preset.repeat}
                  onChange={(e) => {
                    const repeat = e.target.value as Repeat;
                    setValues((v) => {
                      const next: SchedulePreset = { ...v.preset, repeat };
                      if (repeat === 'hours' && !HOUR_STEPS.includes(v.preset.every)) next.every = 6;
                      if (repeat === 'minutes' && !MINUTE_STEPS.includes(v.preset.every)) next.every = 15;
                      if (repeat === 'custom') next.cron = presetToCron(v.preset) ?? v.preset.cron;
                      return { ...v, preset: next };
                    });
                  }}
                >
                  {REPEATS.map(([value, text]) => (
                    <option key={value} value={value}>
                      {text}
                    </option>
                  ))}
                </select>
              </Field>
              {preset.repeat === 'daily' || preset.repeat === 'days' ? (
                <Field label="At" className="trg-narrow">
                  <input type="text" inputMode="numeric" value={preset.time} onChange={(e) => setPreset({ time: e.target.value })} />
                </Field>
              ) : null}
              {preset.repeat === 'hours' ? (
                <Field label="Every">
                  <select value={preset.every} onChange={(e) => setPreset({ every: Number(e.target.value) })}>
                    {HOUR_STEPS.map((n) => (
                      <option key={n} value={n}>
                        {n === 1 ? '1 hour' : `${n} hours`}
                      </option>
                    ))}
                  </select>
                </Field>
              ) : null}
              {preset.repeat === 'minutes' ? (
                <Field label="Every">
                  <select value={preset.every} onChange={(e) => setPreset({ every: Number(e.target.value) })}>
                    {MINUTE_STEPS.map((n) => (
                      <option key={n} value={n}>
                        {`${n} minutes`}
                      </option>
                    ))}
                  </select>
                </Field>
              ) : null}
              {preset.repeat === 'custom' ? (
                <Field
                  label="Cron expression"
                  hint="Five fields: minute, hour, day of month, month, day of week. The server checks it when you save."
                >
                  <input
                    type="text"
                    className="trg-cron-input"
                    spellCheck={false}
                    placeholder="0 5 * * *"
                    value={preset.cron}
                    onChange={(e) => setPreset({ cron: e.target.value })}
                  />
                </Field>
              ) : null}
              <Field label="Time zone">
                <select value={values.timeZone} onChange={(e) => setValues((v) => ({ ...v, timeZone: e.target.value }))}>
                  <option value="">{"Daemon's time zone"}</option>
                  {timeZoneChoices(values.timeZone, browserZone).map((zone) => (
                    <option key={zone} value={zone}>
                      {zone}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
            {preset.repeat === 'days' ? (
              <div className="field">
                <span className="field-label" id={daysId}>
                  Days
                </span>
                <div className="trg-days" role="group" aria-labelledby={daysId}>
                  {DAY_NAMES.map((name, i) => {
                    const day = i + 1;
                    const on = preset.days.includes(day);
                    return (
                      <button
                        key={name}
                        type="button"
                        className={cx('chip', on && 'chip-on')}
                        aria-pressed={on}
                        onClick={() =>
                          setPreset({ days: on ? preset.days.filter((d) => d !== day) : [...preset.days, day].sort((a, b) => a - b) })
                        }
                      >
                        {name}
                      </button>
                    );
                  })}
                </div>
              </div>
            ) : null}
            <div className="callout callout-info trg-preview" role="note">
              {cron !== null ? (
                <>
                  <strong>{firesSentence(cron, values.timeZone)}</strong>
                  {preset.repeat !== 'custom' ? (
                    <span className="trg-cron">
                      Saved as cron <code>{cron}</code>
                    </span>
                  ) : null}
                </>
              ) : (
                <span className="muted">{scheduleProblem(preset)}</span>
              )}
            </div>
          </>
        ) : (
          <>
            <Field label="Label" hint="Only issues with this label fire the trigger. Leave it empty for every new issue.">
              <input type="text" value={values.label} onChange={(e) => setValues((v) => ({ ...v, label: e.target.value }))} />
            </Field>
            <div className="callout callout-info trg-preview" role="note">
              <strong>
                Fires for each new issue {label ? `labelled ${label} ` : ''}in {repo}.
              </strong>{' '}
              Only issues opened by an owner, member or collaborator of the repository count, and only those opened after the
              trigger was last turned on. Issues that issue intake takes and onett&apos;s own friction issues never fire it.
            </div>
          </>
        )}

        <Field
          label="Prompt"
          hint={
            values.kind === 'schedule'
              ? 'What the orchestrator is asked each time this fires.'
              : "What the orchestrator is asked for each new issue. The issue's title and text are added after it, labelled as data."
          }
        >
          <textarea rows={5} value={values.prompt} onChange={(e: ChangeEvent<HTMLTextAreaElement>) => setValues((v) => ({ ...v, prompt: e.target.value }))} />
        </Field>

        <label className="toggle">
          <input type="checkbox" checked={values.enabled} onChange={(e) => setValues((v) => ({ ...v, enabled: e.target.checked }))} />{' '}
          On. Turn it off to keep the trigger without firing it.
        </label>

        {problem ? (
          <p className="error-line" role="alert">
            {problem}
          </p>
        ) : null}
        <ErrorLine error={create.error ?? patchTrigger.error} />
        <div className="row-actions">
          <Button type="submit" variant="primary" disabled={create.isPending || patchTrigger.isPending}>
            {trigger ? 'Save' : 'Create trigger'}
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </form>
    </Modal>
  );
}
