import { describe, expect, it } from 'vitest';
import { formatDate, formatDateTime, formatRelative } from './time';

// Midday UTC, so the local calendar date is in August 2026 in every time zone.
const OLD = new Date('2026-08-07T12:00:00Z');
const NOW = Date.parse('2026-10-27T12:00:00Z');

describe('dates over 30 days old', () => {
  it('spell the month, so day and month cannot be confused', () => {
    const text = formatRelative(OLD, NOW);
    expect(text).toBe(formatDate(OLD));
    expect(text).toMatch(/[A-Za-z]{3}/);
    expect(text).toContain('2026');
    expect(text).not.toMatch(/^\d+[/.-]\d+[/.-]\d+$/);
  });

  it('reads "Aug 7, 2026" for a US reader and "7 Aug 2026" for a British one', () => {
    expect(formatDate(OLD, 'en-US')).toBe('Aug 7, 2026');
    expect(formatDate(OLD, 'en-GB')).toBe('7 Aug 2026');
  });

  it('still counts days up to 30 days', () => {
    expect(formatRelative(new Date(NOW - 29 * 86_400_000), NOW)).toBe('29d ago');
  });

  it('spells the month in the full timestamp too', () => {
    // The hour depends on the machine's time zone; the date part and the seconds do not.
    expect(formatDateTime(OLD, 'en-US')).toMatch(/^Aug 7, 2026, \d{1,2}:\d{2}:00\s[AP]M$/);
    expect(formatDateTime(OLD, 'en-GB')).toMatch(/^7 Aug 2026, \d{2}:\d{2}:00$/);
  });
});
