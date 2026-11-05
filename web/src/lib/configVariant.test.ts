import { describe, expect, it } from 'vitest';
import type { SchemaNode } from '../types';
import { activeVariant, emptyValue, splitOptionsForVariant } from './configDoc';

const field = (key: string): SchemaNode => ({ key, type: 'string' });
const options: SchemaNode = {
  key: 'options',
  type: 'variant',
  discriminator: 'type',
  variants: {
    telegram: { key: '', type: 'section', fields: [field('bot_token_env'), field('state_file')] },
    discord: { key: '', type: 'section', fields: [field('bot_token_env'), field('username')] },
  },
};

describe('activeVariant', () => {
  const draft = { channels: [{ type: 'telegram', options: {} }, { type: 'bogus' }, {}] };

  it('picks the variant the sibling type names', () => {
    expect(activeVariant(options, draft, ['channels', '0'])).toBe(options.variants?.telegram);
  });

  it('is undefined for an unknown or unset type, so the raw editor is used', () => {
    expect(activeVariant(options, draft, ['channels', '1'])).toBeUndefined();
    expect(activeVariant(options, draft, ['channels', '2'])).toBeUndefined();
  });

  it('does not treat inherited object keys as variants', () => {
    expect(activeVariant(options, { type: 'toString' }, [])).toBeUndefined();
  });
});

describe('splitOptionsForVariant', () => {
  const current = { bot_token_env: 'TOKEN', state_file: '/tmp/s.json' };

  it('keeps keys the new variant has and reports the rest as dropped', () => {
    expect(splitOptionsForVariant(current, options.variants?.discord)).toEqual({
      kept: { bot_token_env: 'TOKEN' },
      dropped: ['state_file'],
    });
  });

  it('drops nothing when every key exists in the new variant', () => {
    expect(splitOptionsForVariant({ bot_token_env: 'T' }, options.variants?.discord).dropped).toEqual([]);
  });

  it('keeps everything when the target is unknown', () => {
    expect(splitOptionsForVariant(current, undefined)).toEqual({ kept: current, dropped: [] });
  });

  it('handles missing options', () => {
    expect(splitOptionsForVariant(undefined, options.variants?.discord)).toEqual({ kept: {}, dropped: [] });
  });
});

describe('emptyValue', () => {
  it('starts a variant as an empty map', () => {
    expect(emptyValue(options)).toEqual({});
  });
});
