import { describe, expect, it } from 'vitest';
import { applyChanges } from './configDoc';
import {
  chatChanges,
  existingUserIds,
  findEntry,
  ntfyChanges,
  parseUserIds,
  randomTopic,
  serverProblem,
  tokenEnvFor,
} from './channelSetup';

describe('parseUserIds', () => {
  it('accepts digits separated by commas or spaces', () => {
    expect(parseUserIds('telegram', '42, 7')).toEqual({ ids: ['42', '7'] });
  });
  it('rejects blanks, letters and zero', () => {
    expect(parseUserIds('telegram', '')).toHaveProperty('error');
    expect(parseUserIds('discord', 'bob')).toHaveProperty('error');
    expect(parseUserIds('discord', '0')).toHaveProperty('error');
  });
  it('keeps long Discord snowflakes as strings but refuses unsafe Telegram numbers', () => {
    expect(parseUserIds('discord', '123456789012345678')).toEqual({ ids: ['123456789012345678'] });
    expect(parseUserIds('telegram', '123456789012345678')).toHaveProperty('error');
  });
});

describe('chatChanges', () => {
  it('creates the channels list in the shape init writes (Telegram IDs are numbers)', () => {
    const changes = chatChanges({}, 'telegram', ['42']);
    expect(changes).toEqual([
      {
        op: 'set',
        path: ['channels'],
        value: [{ type: 'telegram', options: { bot_token_env: 'TELEGRAM_BOT_TOKEN', allowed_user_ids: [42] } }],
      },
    ]);
  });
  it('appends after other channels, keeping Discord IDs as strings', () => {
    const doc = { channels: [{ type: 'telegram', options: {} }] };
    const changes = chatChanges(doc, 'discord', ['123456789012345678']);
    expect(changes).toEqual([
      {
        op: 'set',
        path: ['channels', '1'],
        value: { type: 'discord', options: { bot_token_env: 'DISCORD_BOT_TOKEN', allowed_user_ids: ['123456789012345678'] } },
      },
    ]);
  });
  it('edits an existing entry instead of adding a duplicate, keeping its other options and token name', () => {
    const doc = {
      channels: [{ type: 'telegram', options: { bot_token_env: 'MY_TG', allowed_user_ids: [1], state_file: '/s.json' } }],
    };
    const next = applyChanges(doc, chatChanges(doc, 'telegram', ['2', '3'])) as typeof doc;
    expect(next.channels).toHaveLength(1);
    expect(next.channels[0].options).toEqual({ bot_token_env: 'MY_TG', allowed_user_ids: [2, 3], state_file: '/s.json' });
    expect(tokenEnvFor(doc, 'telegram')).toBe('MY_TG');
    expect(existingUserIds(doc, 'telegram')).toBe('1');
  });
});

describe('ntfyChanges', () => {
  const doc = { notify: [{ type: 'log' }] };
  it('appends an ntfy notifier after the log one', () => {
    expect(ntfyChanges(doc, 't-1', '', null)).toEqual([
      { op: 'set', path: ['notify', '1'], value: { type: 'ntfy', options: { topic: 't-1' } } },
    ]);
  });
  it('writes server and token_env when given', () => {
    const [change] = ntfyChanges(doc, 't-1', 'https://ntfy.example', 'NTFY_TOKEN');
    expect(change.value).toEqual({ type: 'ntfy', options: { topic: 't-1', server: 'https://ntfy.example', token_env: 'NTFY_TOKEN' } });
  });
  it('edits the existing notifier and drops a server that was cleared', () => {
    const existing = { notify: [{ type: 'log' }, { type: 'ntfy', options: { topic: 'old', server: 'https://x.example' } }] };
    const next = applyChanges(existing, ntfyChanges(existing, 'new', '', null)) as typeof existing;
    expect(next.notify).toHaveLength(2);
    expect(next.notify[1]).toEqual({ type: 'ntfy', options: { topic: 'new' } });
    expect(findEntry(existing, 'ntfy')?.index).toBe(1);
  });
});

describe('helpers', () => {
  it('makes an unguessable topic', () => {
    expect(randomTopic()).toMatch(/^onett-[0-9a-f]{16}$/);
    expect(randomTopic()).not.toBe(randomTopic());
  });
  it('checks the server URL', () => {
    expect(serverProblem('')).toBeNull();
    expect(serverProblem('https://ntfy.example')).toBeNull();
    expect(serverProblem('ntfy.example')).not.toBeNull();
    expect(serverProblem('ftp://x')).not.toBeNull();
  });
});
