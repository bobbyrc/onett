import { describe, expect, it } from 'vitest';
import { keysFor } from './events';
import { keys } from './keys';
import type { OnettEvent } from './types';

const ev = (type: string, extra: Partial<OnettEvent> = {}): OnettEvent => ({
  id: 1,
  time: '2026-10-27T12:00:00Z',
  type,
  message: '',
  ...extra,
});

describe('keysFor triggers', () => {
  it('maps trigger.updated to the trigger list and that trigger', () => {
    expect(keysFor(ev('trigger.updated', { data: { id: 't1' } }))).toEqual([keys.triggersAll, keys.trigger('t1')]);
  });

  it('maps trigger.deleted to the list, that trigger and every firing list', () => {
    expect(keysFor(ev('trigger.deleted', { data: { id: 't1' } }))).toEqual([
      keys.triggersAll,
      keys.trigger('t1'),
      keys.triggerFiringsAll,
    ]);
  });

  it('maps trigger.fired to the list, the firing lists and that trigger', () => {
    expect(keysFor(ev('trigger.fired', { data: { trigger_id: 't1' } }))).toEqual([
      keys.triggersAll,
      keys.triggerFiringsAll,
      keys.trigger('t1'),
    ]);
  });

  it('maps trigger.fired with no data to the list and the firing lists only', () => {
    expect(keysFor(ev('trigger.fired'))).toEqual([keys.triggersAll, keys.triggerFiringsAll]);
  });
});
