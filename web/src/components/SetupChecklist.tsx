import { Fragment, useState } from 'react';
import { Link } from 'react-router-dom';
import { useCan } from '../auth';
import { cx } from '../lib/format';
import { setupIncomplete, setupSteps } from '../lib/setupChecklist';
import { useAgents, useProjects, useSecrets, useStats } from '../queries';
import { Button } from './Button';

const DISMISSED_KEY = 'onett.setupChecklistDismissed';

function wasDismissed(): boolean {
  try {
    return window.localStorage.getItem(DISMISSED_KEY) === '1';
  } catch {
    return false;
  }
}

/**
 * The first-run steps on the Overview. On a fresh install (no project) they are a row of stations
 * with the next one outlined; once a project exists, `form="strip"` is one line under the lanes
 * that opens to the full list. Both hide themselves once every step is done, or when dismissed.
 */
export function SetupChecklist({ form }: { form: 'stations' | 'strip' }) {
  const owner = useCan('administer');
  const projects = useProjects();
  const agents = useAgents();
  const stats = useStats();
  const secrets = useSecrets(owner);
  const [dismissed, setDismissed] = useState(wasDismissed);
  // Only narrow screens collapse the card; on desktop CSS always shows the list.
  const [open, setOpen] = useState(false);
  if (dismissed || !owner) return null;
  if (projects.data === undefined) return null;
  if (form === 'stations' ? projects.data.length > 0 : projects.data.length === 0) return null;

  const steps = setupSteps({ projects: projects.data, agents: agents.data, stats: stats.data, secrets: secrets.data?.secrets });
  if (!setupIncomplete(steps)) return null;

  const dismiss = () => {
    try {
      window.localStorage.setItem(DISMISSED_KEY, '1');
    } catch {
      // Storage may be blocked; the card then returns on reload.
    }
    setDismissed(true);
  };

  const left = steps.filter((s) => !s.done);
  const nextId = steps.find((s) => !s.done && !s.waits)?.id;

  if (form === 'stations') {
    return (
      <section aria-labelledby="finish-setup">
        <h2 id="finish-setup" className="sr-only">
          Finish setting up onett
        </h2>
        <ol className="ov-stations">
          {steps.map((step, i) => {
            const next = step.id === nextId;
            const state = step.done ? 'done ✓' : step.waits ? 'waits for a project' : next ? 'next' : 'to do';
            return (
              <li key={step.id} className={cx('ov-station', step.done && 'is-done', next && 'is-next')}>
                <span className="ov-station-n">
                  Step {i + 1} · {state}
                </span>
                <span className="ov-station-label">{step.label}</span>
                <span className="ov-station-detail">{step.detail}</span>
                {!step.done && !step.waits ? (
                  <Link className={next ? 'btn btn-primary' : 'btn btn-secondary'} to={step.to}>
                    {step.action}
                  </Link>
                ) : null}
              </li>
            );
          })}
        </ol>
      </section>
    );
  }

  return (
    <section className={cx('card setup-checklist ov-setup-strip', open && 'is-open')} aria-labelledby="finish-setup">
      <div className="ov-setup-row">
        <h2 id="finish-setup">Finish setting up onett</h2>
        <span className="ov-setup-left">
          {steps.length - left.length} of {steps.length} done · left:{' '}
          {left.map((step, i) => (
            <Fragment key={step.id}>
              {i > 0 ? ', ' : null}
              <Link to={step.to}>{step.label}</Link>
            </Fragment>
          ))}
          {left.length === 1 ? ` (${left[0].detail})` : null}
        </span>
        <button type="button" className="link-button small" aria-expanded={open} aria-controls={open ? 'setup-steps' : undefined} onClick={() => setOpen(!open)}>
          {open ? 'Hide steps ▴' : 'Show steps ▾'}
        </button>
        <Button onClick={dismiss}>Dismiss</Button>
      </div>
      {open ? (
        <ul className="setup-steps" id="setup-steps">
          {steps.map((step) => (
            <li key={step.id} className={step.done ? 'is-done' : undefined}>
              <span aria-hidden="true">{step.done ? '✓' : '○'}</span>{' '}
              <Link to={step.to}>{step.label}</Link>
              <span className="muted">
                {' '}
                · {step.done ? 'Done' : 'To do'}. {step.detail}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}
