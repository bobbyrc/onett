// React Query hooks for triggers. They sit beside queries.ts and follow its patterns; the keys
// are in keys.ts and the live events that refresh them are in events.ts.
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import * as api from './api';
import { keys } from './keys';
import { LIST_REFRESH_MS } from './queries';
import type { TriggerCreate, TriggerPatch } from './types';

function invalidate(qc: QueryClient, ...queryKeys: readonly (readonly unknown[])[]): void {
  for (const queryKey of queryKeys) void qc.invalidateQueries({ queryKey });
}

export function useTriggers() {
  return useQuery({ queryKey: keys.triggersAll, queryFn: () => api.listTriggers(), refetchInterval: LIST_REFRESH_MS });
}

export function useTrigger(id: string) {
  return useQuery({ queryKey: keys.trigger(id), queryFn: () => api.getTrigger(id), enabled: id !== '' });
}

/** One trigger's firings, newest first. */
export function useTriggerFirings(id: string) {
  return useQuery({
    queryKey: keys.triggerFirings(id),
    queryFn: () => api.listTriggerFirings(id),
    enabled: id !== '',
    refetchInterval: LIST_REFRESH_MS,
  });
}

/** The newest firings across every trigger. */
export function useAllTriggerFirings() {
  return useQuery({ queryKey: keys.triggerFiringsLog, queryFn: () => api.listAllTriggerFirings(), refetchInterval: LIST_REFRESH_MS });
}

export function useCreateTrigger() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: TriggerCreate) => api.createTrigger(body),
    onSuccess: () => invalidate(qc, keys.triggersAll),
  });
}

export function usePatchTrigger() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { id: string; patch: TriggerPatch }) => api.patchTrigger(vars.id, vars.patch),
    onSuccess: (_trigger, vars) => invalidate(qc, keys.triggersAll, keys.trigger(vars.id)),
  });
}

export function useDeleteTrigger() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteTrigger(id),
    onSuccess: () => invalidate(qc, keys.triggersAll, keys.triggerFiringsAll),
  });
}

export function useRunTrigger() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.runTrigger(id),
    onSuccess: (_firing, id) => invalidate(qc, keys.triggersAll, keys.trigger(id), keys.triggerFiringsAll),
  });
}
