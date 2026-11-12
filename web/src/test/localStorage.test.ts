// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

// The two tests run in order in one file. The shared setup (setup.ts) must wipe storage between
// them; nothing here clears it.
describe('storage is clean between tests', () => {
  it('first test leaves values behind', () => {
    window.localStorage.setItem('onett.test.leak', 'left behind');
    window.sessionStorage.setItem('onett.test.leak', 'left behind');
    expect(window.localStorage.getItem('onett.test.leak')).toBe('left behind');
  });

  it('second test sees none of them', () => {
    expect(window.localStorage.getItem('onett.test.leak')).toBeNull();
    expect(window.sessionStorage.getItem('onett.test.leak')).toBeNull();
    expect(window.localStorage.length).toBe(0);
  });
});
