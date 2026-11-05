// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createFrameBatcher } from './frameBatch';

// A manual rAF: callbacks run only when the test fires a frame.
let frames: Map<number, FrameRequestCallback>;
let nextId: number;
function fireFrame() {
  const pending = [...frames.values()];
  frames.clear();
  pending.forEach((cb) => cb(0));
}

beforeEach(() => {
  vi.useFakeTimers();
  frames = new Map();
  nextId = 1;
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    const id = nextId++;
    frames.set(id, cb);
    return id;
  });
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  Object.defineProperty(document, 'hidden', { value: false, configurable: true });
});

describe('createFrameBatcher', () => {
  it('turns a burst inside one frame into one commit', () => {
    const apply = vi.fn();
    const b = createFrameBatcher<number>(apply);
    b.push([1]);
    b.push([2, 3]);
    b.push([4]);
    expect(apply).not.toHaveBeenCalled();
    fireFrame();
    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply).toHaveBeenCalledWith([1, 2, 3, 4]);
  });

  it('keeps order across frames and drops nothing', () => {
    const seen: number[][] = [];
    const b = createFrameBatcher<number>((x) => seen.push(x));
    b.push([1, 2]);
    fireFrame();
    b.push([3]);
    b.push([4]);
    fireFrame();
    expect(seen).toEqual([[1, 2], [3, 4]]);
    // The backstop timer of an already-flushed frame must not fire a second time.
    vi.advanceTimersByTime(1000);
    expect(seen).toHaveLength(2);
  });

  it('flush applies immediately and leaves nothing for the frame', () => {
    const apply = vi.fn();
    const b = createFrameBatcher<number>(apply);
    b.push([1, 2]);
    b.flush();
    expect(apply).toHaveBeenCalledWith([1, 2]);
    fireFrame();
    vi.advanceTimersByTime(1000);
    expect(apply).toHaveBeenCalledTimes(1);
    b.flush();
    expect(apply).toHaveBeenCalledTimes(1);
  });

  it('cancel drops queued items and the pending frame', () => {
    const apply = vi.fn();
    const b = createFrameBatcher<number>(apply);
    b.push([1]);
    b.cancel();
    fireFrame();
    vi.advanceTimersByTime(1000);
    expect(apply).not.toHaveBeenCalled();
    b.push([2]);
    fireFrame();
    expect(apply).toHaveBeenCalledWith([2]);
  });

  it('flushes on a timer in a hidden tab, where rAF never fires', () => {
    Object.defineProperty(document, 'hidden', { value: true, configurable: true });
    const apply = vi.fn();
    const b = createFrameBatcher<number>(apply);
    b.push([1]);
    b.push([2]);
    expect(frames.size).toBe(0);
    vi.advanceTimersByTime(16);
    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply).toHaveBeenCalledWith([1, 2]);
  });

  it('flushes on the backstop timer when the tab hides after the frame was requested', () => {
    const apply = vi.fn();
    const b = createFrameBatcher<number>(apply);
    b.push([1]);
    vi.advanceTimersByTime(100);
    expect(apply).toHaveBeenCalledWith([1]);
    expect(frames.size).toBe(0);
  });

  it('uses the timer when rAF is unavailable', () => {
    vi.stubGlobal('requestAnimationFrame', undefined);
    const apply = vi.fn();
    const b = createFrameBatcher<number>(apply);
    b.push([1]);
    vi.advanceTimersByTime(16);
    expect(apply).toHaveBeenCalledWith([1]);
  });

  it('ignores empty pushes', () => {
    const b = createFrameBatcher<number>(vi.fn());
    b.push([]);
    expect(frames.size).toBe(0);
  });
});
