import { afterEach, describe, expect, it, vi } from 'vitest';
import type { QueryClient } from '@tanstack/react-query';
import { EventStream } from './events';
import type { WsServerMessage } from './types';

// The live light is 'live' whenever the socket is open and its watches are confirmed, whichever
// came first: the watch or the open.
class Sock {
  static all: Sock[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((m: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor(readonly url: string) {
    Sock.all.push(this);
  }
  send(): void {}
  close(): void {}
  open(): void {
    this.onopen?.();
  }
  receive(msg: WsServerMessage): void {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  drop(): void {
    this.onclose?.();
  }
}
const last = () => Sock.all[Sock.all.length - 1]!;

function setup() {
  vi.useFakeTimers();
  vi.stubGlobal('WebSocket', Sock);
  vi.stubGlobal('location', { protocol: 'http:', host: 'onett.test' });
  const target = new EventTarget();
  vi.stubGlobal('window', target);
  vi.stubGlobal('document', Object.assign(target, { visibilityState: 'visible' }));
  const stream = new EventStream();
  stream.start({ invalidateQueries: vi.fn().mockResolvedValue(undefined) } as unknown as QueryClient);
  return stream;
}

describe('live status and watches', () => {
  let stream: EventStream | null = null;
  afterEach(() => {
    stream?.stop();
    stream = null;
    vi.useRealTimers();
    vi.unstubAllGlobals();
    Sock.all = [];
  });

  it('is live once a watch started before the socket opened is confirmed', async () => {
    stream = setup();
    const w = stream.watch({ conversation_id: 'c1' });
    expect(stream.getStatus()).toBe('reconnecting');
    last().open();
    last().receive({ kind: 'subscribed', conversation_id: 'c1' });
    await w.ready;
    expect(stream.getStatus()).toBe('live');
  });

  it('is live on open when the watch starts after the socket is open', () => {
    stream = setup();
    last().open();
    expect(stream.getStatus()).toBe('live');
    stream.watch({ run_id: 'r1' });
    expect(stream.getStatus()).toBe('live');
  });

  it('shows reconnecting only for a real drop, and is live again after the re-subscribe', async () => {
    stream = setup();
    const w = stream.watch({ run_id: 'r1' });
    last().open();
    last().receive({ kind: 'subscribed', run_id: 'r1' });
    await w.ready;
    expect(stream.getStatus()).toBe('live');

    last().drop();
    expect(stream.getStatus()).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(1_000);
    await vi.advanceTimersByTimeAsync(0);
    last().open();
    expect(stream.getStatus()).toBe('reconnecting');
    last().receive({ kind: 'subscribed', run_id: 'r1' });
    expect(stream.getStatus()).toBe('live');
  });
});
