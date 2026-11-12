// Retries one or several tasks from a board row. Several tasks are retried one after the other, so
// one failure does not stop the rest; the first error is what the owner sees.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { retryTask } from '../api';
import { useCan } from '../auth';
import { keys } from '../keys';
import { errorMessage } from '../lib/format';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';

export function RetryTasksButton({ ids, label, confirm = false }: { ids: string[]; label: string; confirm?: boolean }) {
  const canOperate = useCan('operate');
  const client = useQueryClient();
  const mutation = useMutation({
    mutationFn: async () => {
      let failed = 0;
      let first: unknown = null;
      for (const id of ids) {
        try {
          await retryTask(id);
        } catch (err) {
          failed++;
          first ??= err;
        }
      }
      for (const queryKey of [keys.problemsAll, keys.tasksAll, keys.stats]) {
        void client.invalidateQueries({ queryKey });
      }
      if (failed === 0) return;
      const text = errorMessage(first);
      throw new Error(ids.length === 1 ? text : `${failed} of ${ids.length} could not be retried: ${text}`);
    },
  });

  if (!canOperate || ids.length === 0) return null;
  return (
    <>
      <Button variant="secondary" confirm={confirm} disabled={mutation.isPending} onClick={() => mutation.mutate()}>
        {label}
      </Button>
      <ErrorLine error={mutation.error} />
    </>
  );
}
