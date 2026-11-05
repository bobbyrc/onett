// Pure logic of the guided Telegram / Discord / ntfy setup. It builds the config changes the
// wizard sends, in the same shape `onett init` writes (see internal/config/setup.go). Nothing
// here touches a token: a token is named by the environment variable it is stored under.
import type { ConfigChange } from '../types';
import { getAt } from './configDoc';

export type SetupKind = 'telegram' | 'discord' | 'ntfy';
export type ChatKind = 'telegram' | 'discord';

/** Where the entry for each kind lives in the config. */
export function listKey(kind: SetupKind): 'channels' | 'notify' {
  return kind === 'ntfy' ? 'notify' : 'channels';
}

/** The variable a kind's token is read from when the config does not already name one. */
export function defaultTokenEnv(kind: SetupKind): string {
  return kind === 'ntfy' ? 'NTFY_TOKEN' : `${kind.toUpperCase()}_BOT_TOKEN`;
}

export interface FoundEntry {
  index: number;
  options: Record<string, unknown>;
}

/** The first entry of `kind` in the document, or null. Only the first of each type is used. */
export function findEntry(document: unknown, kind: SetupKind): FoundEntry | null {
  const list = getAt(document, [listKey(kind)]);
  if (!Array.isArray(list)) return null;
  const index = list.findIndex((e) => typeof e === 'object' && e !== null && (e as { type?: unknown }).type === kind);
  if (index < 0) return null;
  const options = (list[index] as { options?: unknown }).options;
  return { index, options: typeof options === 'object' && options !== null ? (options as Record<string, unknown>) : {} };
}

/** The variable name the token is stored under: the entry's own, else the default. */
export function tokenEnvFor(document: unknown, kind: SetupKind): string {
  const key = kind === 'ntfy' ? 'token_env' : 'bot_token_env';
  const existing = findEntry(document, kind)?.options[key];
  return typeof existing === 'string' && existing.trim() !== '' ? existing.trim() : defaultTokenEnv(kind);
}

/** The allowed user IDs of an existing chat entry, as text for the form. */
export function existingUserIds(document: unknown, kind: ChatKind): string {
  const ids = findEntry(document, kind)?.options.allowed_user_ids;
  return Array.isArray(ids) ? ids.map(String).join(', ') : '';
}

/** Splits "1, 2" into IDs and checks each is whole digits (Telegram IDs must also be safe numbers). */
export function parseUserIds(kind: ChatKind, input: string): { ids: string[] } | { error: string } {
  const ids = input
    .split(/[,\s]+/)
    .map((s) => s.trim())
    .filter(Boolean);
  if (ids.length === 0) return { error: 'Enter your user ID.' };
  for (const id of ids) {
    if (!/^[0-9]+$/.test(id) || /^0+$/.test(id)) return { error: `"${id}" is not a user ID: use digits only.` };
    if (kind === 'telegram' && !Number.isSafeInteger(Number(id))) return { error: `"${id}" is too long for a Telegram user ID.` };
  }
  return { ids };
}

/**
 * The changes that connect a chat bot. An existing entry of the kind is edited in place (its
 * other options, such as state_file, stay); otherwise a new one is appended.
 */
export function chatChanges(document: unknown, kind: ChatKind, ids: string[]): ConfigChange[] {
  const value = kind === 'telegram' ? ids.map(Number) : ids;
  const found = findEntry(document, kind);
  const tokenEnv = tokenEnvFor(document, kind);
  if (found) {
    const base = ['channels', String(found.index), 'options'];
    return [
      { op: 'set', path: [...base, 'bot_token_env'], value: tokenEnv },
      { op: 'set', path: [...base, 'allowed_user_ids'], value },
    ];
  }
  const entry = { type: kind, options: { bot_token_env: tokenEnv, allowed_user_ids: value } };
  return appendChange(document, 'channels', entry);
}

/**
 * The changes that write an ntfy notifier. `tokenEnv` is the variable holding an access token, or
 * null for a public topic (an existing token_env is then left alone). An empty server means the
 * default, https://ntfy.sh, so a stored server is removed.
 */
export function ntfyChanges(document: unknown, topic: string, server: string, tokenEnv: string | null): ConfigChange[] {
  const found = findEntry(document, 'ntfy');
  if (found) {
    const base = ['notify', String(found.index), 'options'];
    const changes: ConfigChange[] = [{ op: 'set', path: [...base, 'topic'], value: topic }];
    if (server) changes.push({ op: 'set', path: [...base, 'server'], value: server });
    else if ('server' in found.options) changes.push({ op: 'delete', path: [...base, 'server'] });
    if (tokenEnv) changes.push({ op: 'set', path: [...base, 'token_env'], value: tokenEnv });
    return changes;
  }
  const options: Record<string, unknown> = { topic };
  if (server) options.server = server;
  if (tokenEnv) options.token_env = tokenEnv;
  return appendChange(document, 'notify', { type: 'ntfy', options });
}

function appendChange(document: unknown, key: 'channels' | 'notify', entry: unknown): ConfigChange[] {
  const list = getAt(document, [key]);
  // A missing list is created with the entry; an existing one gets it at its end.
  return Array.isArray(list)
    ? [{ op: 'set', path: [key, String(list.length)], value: entry }]
    : [{ op: 'set', path: [key], value: [entry] }];
}

/** A topic nobody can guess: "onett-" and 16 random hex digits (the same as `onett init`). */
export function randomTopic(bytes: (n: number) => Uint8Array = (n) => crypto.getRandomValues(new Uint8Array(n))): string {
  return 'onett-' + Array.from(bytes(8), (b) => b.toString(16).padStart(2, '0')).join('');
}

/** A server URL must be http(s); blank is fine (the default). */
export function serverProblem(server: string): string | null {
  if (server === '') return null;
  try {
    const u = new URL(server);
    return u.protocol === 'http:' || u.protocol === 'https:' ? null : 'Use an http:// or https:// address.';
  } catch {
    return 'That is not a web address.';
  }
}
