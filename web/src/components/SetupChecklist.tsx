import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useCan } from '../auth';
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

/** The first-run card on the Overview. It hides itself once every step is done, or when dismissed. */
export function SetupChecklist() {
  const owner = useCan('administer');
  const projects = useProjects();
  const agents = useAgents();
  const stats = useStats();
  const secrets = useSecrets(owner);
  const [dismissed, setDismissed] = useState(wasDismissed);
  if (dismissed || !owner) return null;

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

  return (
    <section className="card setup-checklist" aria-labelledby="finish-setup">
      <h2 id="finish-setup">Finish setting up onett</h2>
      <ul className="setup-steps">
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
      <Button onClick={dismiss}>Dismiss</Button>
    </section>
  );
}
