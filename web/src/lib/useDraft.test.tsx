// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { draftKey, useDraft } from './useDraft';

beforeEach(() => {
  vi.useFakeTimers();
  window.localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('useDraft', () => {
  it('saves after a pause, under onett.chat.draft.<key>', () => {
    const { result } = renderHook(() => useDraft('c1'));
    act(() => result.current[1]('hello'));
    expect(window.localStorage.getItem('onett.chat.draft.c1')).toBeNull();
    act(() => void vi.advanceTimersByTime(300));
    expect(window.localStorage.getItem('onett.chat.draft.c1')).toBe('hello');
    expect(draftKey('task:t1')).toBe('onett.chat.draft.task:t1');
  });

  it('restores after a remount', () => {
    window.localStorage.setItem('onett.chat.draft.c1', 'kept');
    const { result } = renderHook(() => useDraft('c1'));
    expect(result.current[0]).toBe('kept');
  });

  it('keeps one draft per conversation key, also when the key changes in place', () => {
    window.localStorage.setItem('onett.chat.draft.task:t1', 'task draft');
    const { result, rerender } = renderHook(({ k }) => useDraft(k), { initialProps: { k: 'c1' } });
    act(() => result.current[1]('first'));
    rerender({ k: 'task:t1' });
    expect(result.current[0]).toBe('task draft');
    expect(window.localStorage.getItem('onett.chat.draft.c1')).toBe('first'); // saved on the switch
    rerender({ k: 'c1' });
    expect(result.current[0]).toBe('first');
  });

  it('saves what is pending on unmount', () => {
    const { result, unmount } = renderHook(() => useDraft('c1'));
    act(() => result.current[1]('late'));
    unmount();
    expect(window.localStorage.getItem('onett.chat.draft.c1')).toBe('late');
  });

  it('clear removes the stored draft and cancels a pending save', () => {
    const { result } = renderHook(() => useDraft('c1'));
    act(() => result.current[1]('sent'));
    act(() => result.current[2]());
    act(() => void vi.advanceTimersByTime(1000));
    expect(result.current[0]).toBe('');
    expect(window.localStorage.getItem('onett.chat.draft.c1')).toBeNull();
  });

  it('survives a localStorage that throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const { result } = renderHook(() => useDraft('c1'));
    expect(result.current[0]).toBe('');
    act(() => result.current[1]('in memory'));
    act(() => void vi.advanceTimersByTime(300));
    expect(result.current[0]).toBe('in memory');
    act(() => result.current[2]());
    expect(result.current[0]).toBe('');
  });
});
