import { describe, expect, it } from 'vitest';
import type { ConversationSettings, ConversationSettingsView } from '../types';
import {
  contextFraction,
  contextWindow,
  modelEffortLabel,
  needsDangerConfirm,
  permissionLabel,
} from './composerSettings';

const view = (settings = {}, defaults: ConversationSettings = { model: 'sonnet' }): ConversationSettingsView => ({
  settings,
  defaults,
  harness: 'claude',
  choices: {
    models: [{ value: 'opus', label: 'Opus' }, { value: 'sonnet', label: 'Sonnet' }],
    efforts: [{ value: 'high', label: 'High' }],
    permission_modes: [
      { value: 'auto', label: 'Auto' },
      { value: 'bypassPermissions', label: 'Bypass permissions', dangerous: true },
    ],
  },
});

describe('composer settings labels', () => {
  it('shows the role\'s effort and permission defaults until the conversation picks its own', () => {
    const defaults = { model: 'sonnet', effort: 'high', permission_mode: 'auto' };
    expect(modelEffortLabel(view({}, defaults))).toBe('Sonnet · High');
    expect(permissionLabel(view({}, defaults))).toBe('Auto');
    expect(permissionLabel(view({ permission_mode: 'bypassPermissions' }, defaults))).toBe('Bypass permissions');
  });

  it('joins model and effort, falling back to the role default model', () => {
    expect(modelEffortLabel(view({ model: 'opus', effort: 'high' }))).toBe('Opus · High');
    expect(modelEffortLabel(view())).toBe('Sonnet');
    expect(modelEffortLabel(view({ model: 'custom-1' }))).toBe('custom-1');
  });

  it('names the permission mode, or the default', () => {
    expect(permissionLabel(view({ permission_mode: 'auto' }))).toBe('Auto');
    expect(permissionLabel(view())).toBe('Default permissions');
  });

  it('asks for confirmation only when a dangerous mode is newly chosen', () => {
    expect(needsDangerConfirm(view(), 'bypassPermissions')).toBe(true);
    expect(needsDangerConfirm(view({ permission_mode: 'bypassPermissions' }), 'bypassPermissions')).toBe(false);
    expect(needsDangerConfirm(view(), 'auto')).toBe(false);
  });
});

describe('context ring', () => {
  it('knows Claude windows and hides the ring for unknown models', () => {
    expect(contextWindow('opus')).toBe(200_000);
    expect(contextWindow('claude-opus-4[1m]')).toBe(1_000_000);
    expect(contextWindow('gpt-x')).toBeNull();
    expect(contextWindow(undefined)).toBeNull();
  });

  it('is the last request context over the window, capped at 1', () => {
    expect(contextFraction({ context_tokens: 50_000, model: 'sonnet' })).toBeCloseTo(0.25);
    expect(contextFraction({ context_tokens: 900_000, model: 'sonnet' })).toBe(1);
    expect(contextFraction({ context_tokens: 0, model: 'sonnet' })).toBeNull();
    expect(contextFraction({ model: 'sonnet' })).toBeNull();
    expect(contextFraction(undefined)).toBeNull();
    expect(contextFraction({ context_tokens: 10, model: 'gpt-x' })).toBeNull();
  });

  it('ignores input_tokens summed over a many-request run', () => {
    // A 12-request turn re-reads a ~58k prompt each time: input_tokens is ~700k, the context is 58k.
    const run = { input_tokens: 700_000, context_tokens: 58_679, model: 'sonnet' };
    expect(contextFraction(run)).toBeCloseTo(0.293, 2);
    // No context_tokens (an old run) shows no ring rather than an overstated one.
    expect(contextFraction({ input_tokens: 700_000, model: 'sonnet' } as never)).toBeNull();
  });
});
