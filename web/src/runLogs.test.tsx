// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { OnettEvent, LogLine } from './types';

type Listener = (ev: OnettEvent) => void;
const listeners = new Set<Listener>();

vi.mock('./events', () => ({
  subscribe: (l: Listener) => {
    listeners.add(l);
    return () => listeners.delete(l);
  },
  watch: () => ({ ready: Promise.resolve(), release: () => {} }),
  useLiveStatus: () => 'live',
}));
vi.mock('./api', () => ({ getRunLogs: vi.fn(async () => []) }));

import { useRunLogLines } from './runLogs';

let frames: FrameRequestCallback[] = [];
const fireFrame = () => {
  const pending = frames;
  frames = [];
  act(() => pending.forEach((cb) => cb(0)));
};

const line = (seq: number, run_id = 'r1'): LogLine => ({ seq, run_id, time: '', kind: 'text', text: `l${seq}` });
const emit = (lines: LogLine[]) =>
  act(() => listeners.forEach((l) => l({ id: 0, time: '', type: 'run.log', message: '', data: lines })));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
  frames = [];
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => frames.push(cb));
  vi.stubGlobal('cancelAnimationFrame', () => {
    frames = [];
  });
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  listeners.clear();
});

async function mount(runId = 'r1', active = true) {
  let renders = 0;
  const hook = renderHook(
    (p: { runId: string; active: boolean }) => {
      renders++;
      return useRunLogLines(p.runId, p.active);
    },
    { initialProps: { runId, active } },
  );
  await act(async () => {}); // initial load settles
  return { hook, renders: () => renders, reset: () => (renders = 0) };
}

describe('useRunLogLines batching', () => {
  it('applies a burst as one state commit, in order', async () => {
    const m = await mount();
    m.reset();
    for (let i = 1; i <= 50; i++) emit([line(i)]);
    expect(m.hook.result.current.lines).toHaveLength(0);
    fireFrame();
    expect(m.hook.result.current.lines.map((l) => l.seq)).toEqual(Array.from({ length: 50 }, (_, i) => i + 1));
    expect(m.renders()).toBe(1);
  });

  it('drops duplicates across frames', async () => {
    const m = await mount();
    emit([line(1), line(2)]);
    fireFrame();
    emit([line(2), line(3)]);
    fireFrame();
    expect(m.hook.result.current.lines.map((l) => l.seq)).toEqual([1, 2, 3]);
  });

  it('flushes at once when the run stops being active', async () => {
    const m = await mount();
    emit([line(1)]);
    m.hook.rerender({ runId: 'r1', active: false });
    expect(m.hook.result.current.lines.map((l) => l.seq)).toEqual([1]);
  });

  it('unmount cancels the pending frame', async () => {
    const m = await mount();
    emit([line(1)]);
    m.hook.unmount();
    expect(frames).toHaveLength(0);
    expect(() => vi.advanceTimersByTime(1000)).not.toThrow();
  });

  it('a run change drops queued lines of the old run', async () => {
    const m = await mount();
    emit([line(1)]);
    m.hook.rerender({ runId: 'r2', active: true });
    await act(async () => {});
    vi.advanceTimersByTime(1000);
    fireFrame();
    expect(m.hook.result.current.lines).toEqual([]);
  });

  it('flushes on the timer in a hidden tab', async () => {
    Object.defineProperty(document, 'hidden', { value: true, configurable: true });
    const m = await mount();
    emit([line(1), line(2)]);
    act(() => void vi.advanceTimersByTime(16));
    expect(m.hook.result.current.lines.map((l) => l.seq)).toEqual([1, 2]);
    Object.defineProperty(document, 'hidden', { value: false, configurable: true });
  });
});

describe('render count for a 500 line stream', () => {
  it('commits once per frame, not once per event', async () => {
    const m = await mount();
    m.reset();
    // 500 events in 10 bursts of 50; a frame between bursts.
    for (let burst = 0; burst < 10; burst++) {
      for (let i = 0; i < 50; i++) emit([line(burst * 50 + i + 1)]);
      fireFrame();
    }
    expect(m.hook.result.current.lines).toHaveLength(500);
    // Before batching this was 500 (one setState per event). Recorded in the PR.
    console.info(`renders for 500 events: ${m.renders()}`);
    expect(m.renders()).toBe(10);
  });
});
