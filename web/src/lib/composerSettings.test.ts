import { describe, expect, it } from 'vitest';
import type { ConversationSettingsView } from '../types';
import {
  contextFraction,
  contextWindow,
  modelEffortLabel,
  needsDangerConfirm,
  permissionLabel,
} from './composerSettings';

const view = (settings = {}): ConversationSettingsView => ({
  settings,
  defaults: { model: 'sonnet' },
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

  it('is input tokens over the window, capped at 1', () => {
    expect(contextFraction({ input_tokens: 50_000, model: 'sonnet' })).toBeCloseTo(0.25);
    expect(contextFraction({ input_tokens: 900_000, model: 'sonnet' })).toBe(1);
    expect(contextFraction({ input_tokens: 0, model: 'sonnet' })).toBeNull();
    expect(contextFraction(undefined)).toBeNull();
    expect(contextFraction({ input_tokens: 10, model: 'gpt-x' })).toBeNull();
  });
});
