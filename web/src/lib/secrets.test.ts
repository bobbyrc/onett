import { describe, expect, it } from 'vitest';
import type { SecretView } from '../types';
import {
  SECRET_NAME_RE,
  isShadowed,
  pickerNames,
  secretStatus,
  sortSecrets,
  sourceLabel,
  valueProblem,
} from './secrets';

const view = (over: Partial<SecretView>): SecretView => ({
  name: 'X',
  set: false,
  source: '',
  referenced_by: [],
  ...over,
});

describe('secret wording', () => {
  it('says set or not set', () => {
    expect(secretStatus(view({ set: true, source: 'file' }))).toBe('set');
    expect(secretStatus(view({}))).toBe('not set');
  });

  it('names the source', () => {
    expect(sourceLabel(view({ set: true, source: 'file' }))).toBe('secrets.env');
    expect(sourceLabel(view({ set: true, source: 'environment' }))).toBe("daemon's environment");
    expect(sourceLabel(view({}))).toBe('');
  });

  it('flags a value the environment overrides', () => {
    expect(isShadowed(view({ set: true, source: 'environment' }))).toBe(true);
    expect(isShadowed(view({ set: true, source: 'file' }))).toBe(false);
  });
});

describe('valueProblem', () => {
  it('refuses empty and multi-line values', () => {
    expect(valueProblem('')).not.toBeNull();
    expect(valueProblem('a\nb')).not.toBeNull();
    expect(valueProblem('abc')).toBeNull();
    expect(valueProblem(' padded ')).toBeNull();
  });
});

describe('sortSecrets', () => {
  it('puts referenced-but-unset names first, then set ones, then the rest, by name', () => {
    const sorted = sortSecrets([
      view({ name: 'B_UNUSED' }),
      view({ name: 'A_SET', set: true, source: 'file' }),
      view({ name: 'C_NEEDED', referenced_by: ['channels[0].options.bot_token_env'] }),
      view({ name: 'A_NEEDED', referenced_by: ['x'] }),
    ]);
    expect(sorted.map((s) => s.name)).toEqual(['A_NEEDED', 'C_NEEDED', 'A_SET', 'B_UNUSED']);
  });
});

describe('secret picker', () => {
  it('lists known names sorted and keeps an unknown current value', () => {
    const list = [view({ name: 'B' }), view({ name: 'A' })];
    expect(pickerNames(list, 'A')).toEqual(['A', 'B']);
    expect(pickerNames(list, 'ODD')).toEqual(['A', 'B', 'ODD']);
    expect(pickerNames(list, '')).toEqual(['A', 'B']);
  });

  it('checks secret names', () => {
    expect(SECRET_NAME_RE.test('TELEGRAM_BOT_TOKEN')).toBe(true);
    expect(SECRET_NAME_RE.test('telegram')).toBe(false);
    expect(SECRET_NAME_RE.test('1ABC')).toBe(false);
  });
});
