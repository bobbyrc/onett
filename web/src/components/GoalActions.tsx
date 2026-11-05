import { useState } from 'react';
import type { Goal } from '../types';
import { useCan } from '../auth';
import { useSetGoalStatus } from '../queries';
import { goalActions, needsText, type GoalAction } from '../lib/goals';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { Field } from './Field';

const LABELS: Record<GoalAction, string> = {
  pause: 'Pause',
  resume: 'Resume',
  done: 'Mark done',
  block: 'Block',
};

const TEXT_LABELS: Partial<Record<GoalAction, string>> = {
  done: 'Summary: what was achieved',
  block: 'Reason for blocking',
};

/** Pause, resume, done and block. Done and block ask for a written summary or reason first. */
export function GoalActions({ goal }: { goal: Goal }) {
  const set = useSetGoalStatus(goal.id);
  const canOperate = useCan('operate');
  const [asking, setAsking] = useState<GoalAction | null>(null);
  const [text, setText] = useState('');

  const actions = goalActions(goal.status);
  if (!canOperate || actions.length === 0) return null;

  const run = (action: GoalAction, body = '') =>
    set.mutate(
      { action, text: body },
      {
        onSuccess: () => {
          setAsking(null);
          setText('');
        },
      },
    );

  if (asking) {
    return (
      <form
        className="inline-form"
        onSubmit={(event) => {
          event.preventDefault();
          run(asking, text.trim());
        }}
      >
        <Field label={TEXT_LABELS[asking] ?? ''}>
          <textarea rows={2} value={text} onChange={(event) => setText(event.target.value)} />
        </Field>
        <div className="row-actions">
          <Button type="submit" variant="primary" disabled={set.isPending || text.trim() === ''}>
            {LABELS[asking]}
          </Button>
          <Button variant="ghost" onClick={() => setAsking(null)}>
            Dismiss
          </Button>
        </div>
        <ErrorLine error={set.error} />
      </form>
    );
  }
  return (
    <>
      {actions.map((action) => (
        <Button
          key={action}
          variant={action === 'resume' ? 'primary' : 'secondary'}
          disabled={set.isPending}
          onClick={() => (needsText(action) ? setAsking(action) : run(action))}
        >
          {LABELS[action]}
        </Button>
      ))}
      <ErrorLine error={set.error} />
    </>
  );
}
