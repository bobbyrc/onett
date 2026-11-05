import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { deleteSecret, listSecrets, putSecret } from './api';

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal('window', new EventTarget());
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const json = (status: number, body?: unknown) =>
  body === undefined
    ? new Response(null, { status })
    : new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

describe('secrets api', () => {
  it('lists names without values', async () => {
    fetchMock.mockResolvedValue(
      json(200, { secrets: [{ name: 'TELEGRAM_BOT_TOKEN', set: true, source: 'file', referenced_by: [] }] }),
    );
    const got = await listSecrets();
    expect(got.secrets[0]!.name).toBe('TELEGRAM_BOT_TOKEN');
    expect(fetchMock.mock.calls[0]![0]).toContain('/secrets');
  });

  it('puts the value in the body and the escaped name in the path', async () => {
    fetchMock.mockResolvedValue(json(200, { name: 'A', set: true, shadowed: false }));
    await putSecret('A_B', 'v');
    const [url, init] = fetchMock.mock.calls[0]! as [string, RequestInit];
    expect(url).toContain('/secrets/A_B');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body as string)).toEqual({ value: 'v' });
  });

  it('deletes by name', async () => {
    fetchMock.mockResolvedValue(json(204));
    await deleteSecret('A_B');
    const [url, init] = fetchMock.mock.calls[0]! as [string, RequestInit];
    expect(url).toContain('/secrets/A_B');
    expect(init.method).toBe('DELETE');
  });
});
