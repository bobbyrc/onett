import { describe, expect, it } from 'vitest';
import { feedRows } from './activity';
import type { OnettEvent } from '../types';

// Local-time constructors, so the expected clock times do not depend on the machine's zone.
const at = (h: number, m: number, s = 0, day = 12) => new Date(2026, 10, day, h, m, s);
const NOW = at(13, 42).getTime();

let nextId = 1;
const ev = (type: string, date: Date, extra: Partial<OnettEvent> = {}): OnettEvent => ({
  id: nextId++,
  time: date.toISOString(),
  type,
  message: '',
  ...extra,
});

const config = (date: Date, message = 'config reloaded from /tmp/onett.yaml') => ev('system', date, { message });

describe('feedRows, plain rows that repeat', () => {
  it('folds five identical system messages within two minutes into one row with a count', () => {
    const events = [
      config(at(10, 0, 0)),
      config(at(10, 0, 30)),
      config(at(10, 1, 0)),
      config(at(10, 1, 30)),
      config(at(10, 2, 0)),
    ];
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(1);
    expect(rows[0].parts).toEqual([{ text: 'Config reloaded from /tmp/onett.yaml' }]);
    expect(rows[0].note).toMatch(/^5 times/);
  });

  it('keeps two system messages with different text as two rows with no count', () => {
    const events = [config(at(10, 0), 'config reloaded from /tmp/onett.yaml'), config(at(10, 1), 'backup written')];
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(2);
    expect(rows.every((row) => row.note === undefined)).toBe(true);
  });

  it('keeps two identical system messages eleven minutes apart as two rows', () => {
    const events = [config(at(10, 0)), config(at(10, 11))];
    const rows = feedRows(events, [], NOW);
    expect(rows).toHaveLength(2);
    expect(rows.every((row) => row.note === undefined)).toBe(true);
  });
});
