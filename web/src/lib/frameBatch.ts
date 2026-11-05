/**
 * Collects items and hands them over in one batch per animation frame, so a fast stream causes one
 * state update (and one render) per frame instead of one per item.
 *
 * `requestAnimationFrame` does not fire in a hidden tab, so a short timer is armed beside it and
 * whichever fires first flushes; a background tab still catches up. Where rAF does not exist only
 * the timer is used. Items reach `apply` in the order they were pushed.
 */
export interface FrameBatcher<T> {
  /** Queues items for the next frame. */
  push(items: readonly T[]): void;
  /** Applies everything queued now and cancels the pending frame. Does nothing when empty. */
  flush(): void;
  /** Drops everything queued and cancels the pending frame. */
  cancel(): void;
}

/** Backstop for hidden tabs, where rAF is paused. About one frame at 60Hz. */
export const FRAME_FALLBACK_MS = 16;

export function createFrameBatcher<T>(apply: (items: T[]) => void): FrameBatcher<T> {
  let queue: T[] = [];
  let raf: number | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const unschedule = () => {
    if (raf !== undefined) cancelAnimationFrame(raf);
    if (timer !== undefined) clearTimeout(timer);
    raf = undefined;
    timer = undefined;
  };

  const flush = () => {
    unschedule();
    if (queue.length === 0) return;
    const batch = queue;
    queue = [];
    apply(batch);
  };

  return {
    push(items) {
      if (items.length === 0) return;
      queue.push(...items);
      if (raf !== undefined || timer !== undefined) return;
      if (typeof requestAnimationFrame === 'function' && typeof document !== 'undefined' && !document.hidden) {
        raf = requestAnimationFrame(flush);
        // The tab can be hidden after the frame was requested; the timer then still flushes.
        timer = setTimeout(flush, FRAME_FALLBACK_MS * 4);
      } else {
        timer = setTimeout(flush, FRAME_FALLBACK_MS);
      }
    },
    flush,
    cancel() {
      unschedule();
      queue = [];
    },
  };
}
