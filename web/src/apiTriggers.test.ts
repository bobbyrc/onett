import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createTrigger,
  deleteTrigger,
  getTrigger,
  listAllTriggerFirings,
  listTriggerFirings,
  listTriggers,
  patchTrigger,
  runTrigger,
} from './api';

// api.ts reads `window` only on a 401, so a plain EventTarget stands in for it.
let fetchMock: ReturnType<typeof vi.fn>;

function respond(status: number, body?: unknown): Response {
  if (body === undefined) return new Response(null, { status });
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function sentCall(call = 0): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[call]! as [string, RequestInit];
  return { url, method: init.method!, body: init.body === undefined ? undefined : JSON.parse(init.body as string) };
}

beforeEach(() => {
  vi.stubGlobal('window', new EventTarget());
  fetchMock = vi.fn().mockResolvedValue(respond(200, {}));
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('trigger api calls', () => {
  it('lists triggers with a GET', async () => {
    await listTriggers();
    expect(sentCall()).toEqual({ url: '/api/v1/triggers', method: 'GET', body: undefined });
  });

  it('gets one trigger by its encoded ID', async () => {
    await getTrigger('a b');
    expect(sentCall()).toEqual({ url: '/api/v1/triggers/a%20b', method: 'GET', body: undefined });
  });

  it('creates a trigger with a POST of the JSON body', async () => {
    const body = { name: 'nightly', kind: 'schedule' as const, project: 'onett', prompt: 'look' };
    await createTrigger(body);
    expect(sentCall()).toEqual({ url: '/api/v1/triggers', method: 'POST', body });
  });

  it('patches only the given fields with a PATCH', async () => {
    await patchTrigger('t1', { enabled: false });
    expect(sentCall()).toEqual({ url: '/api/v1/triggers/t1', method: 'PATCH', body: { enabled: false } });
  });

  it('deletes a trigger with a DELETE', async () => {
    fetchMock.mockResolvedValue(respond(204));
    await deleteTrigger('t1');
    expect(sentCall()).toEqual({ url: '/api/v1/triggers/t1', method: 'DELETE', body: undefined });
  });

  it('runs a trigger with a POST to its run path', async () => {
    await runTrigger('t1');
    expect(sentCall()).toEqual({ url: '/api/v1/triggers/t1/run', method: 'POST', body: undefined });
  });

  it('lists one trigger firings with the default limit of 200', async () => {
    await listTriggerFirings('t1');
    expect(sentCall()).toEqual({ url: '/api/v1/triggers/t1/firings?limit=200', method: 'GET', body: undefined });
  });

  it('lists all firings with the default limit of 50', async () => {
    await listAllTriggerFirings();
    expect(sentCall()).toEqual({ url: '/api/v1/trigger-firings?limit=50', method: 'GET', body: undefined });
  });
});
