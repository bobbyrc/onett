import type { ConversationSettingsView, Run, SettingsChoice } from '../types';

/** The label of a value: the choice's own label, else the raw value (a configured model has no alias). */
export function choiceLabel(list: SettingsChoice[], value: string | undefined): string {
  if (!value) return '';
  return list.find((c) => c.value === value)?.label ?? value;
}

/** What the model button shows, e.g. "Opus · High". An unset part falls back to the role's default. */
export function modelEffortLabel(view: ConversationSettingsView): string {
  const model = choiceLabel(view.choices.models, view.settings.model || view.defaults.model);
  const effort = choiceLabel(view.choices.efforts, view.settings.effort || view.defaults.effort);
  return [model || 'Default model', effort].filter(Boolean).join(' · ');
}

/** The permission picker's label; the harness's own default mode when none was chosen. */
export function permissionLabel(view: ConversationSettingsView): string {
  return choiceLabel(view.choices.permission_modes, view.settings.permission_mode || view.defaults.permission_mode) || 'Default permissions';
}

/** True when choosing mode needs the "skips safety checks" confirmation: it is dangerous and not already set. */
export function needsDangerConfirm(view: ConversationSettingsView, mode: string): boolean {
  const dangerous = view.choices.permission_modes.find((c) => c.value === mode)?.dangerous;
  return !!dangerous && view.settings.permission_mode !== mode;
}

/**
 * The window to assume for a run that did not report one (a run from before onett stored it).
 * Only families whose window is fixed are named: a Claude alias such as "opus" can mean a 200k or
 * a 1M model, so it gets no ring rather than a wrong one.
 */
export function contextWindow(model: string | undefined): number | null {
  if (!model) return null;
  const m = model.toLowerCase();
  if (m.includes('[1m]') || m.includes('-1m')) return 1_000_000;
  if (/(gpt-5|codex)/.test(m)) return 400_000;
  return null;
}

type ContextRun = Pick<Run, 'context_tokens' | 'model'> & { context_window?: number };

/**
 * How full the context window was after the run: the prompt size of its last model request
 * (context_tokens: input plus cache reads and writes) over the window the CLI reported for the
 * model (context_window), 0..1. Not input_tokens, which sums every request of the run. Older
 * runs have no context_window and fall back to the name table. Null when either is unknown.
 */
export function contextFraction(run: ContextRun | undefined, model?: string): number | null {
  if (!run || !run.context_tokens || run.context_tokens < 0) return null;
  const window = run.context_window && run.context_window > 0 ? run.context_window : contextWindow(model || run.model);
  if (!window) return null;
  return Math.min(1, run.context_tokens / window);
}

/**
 * The fill of the newest run that has one. A run in progress has no context_tokens until it
 * ends, so the ring keeps the previous run's value through the turn instead of going blank.
 */
export function latestContextFraction(runs: Array<ContextRun | undefined>): number | null {
  for (let i = runs.length - 1; i >= 0; i--) {
    const f = contextFraction(runs[i]);
    if (f != null) return f;
  }
  return null;
}
