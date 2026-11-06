// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { ContextRing } from './ContextRing';

afterEach(cleanup);

describe('ContextRing', () => {
  it('draws nothing without usage data', () => {
    for (const f of [null, 0, NaN, Infinity]) {
      const { container } = render(<ContextRing fraction={f} />);
      expect(container.firstChild).toBeNull();
      cleanup();
    }
  });
  it('is neutral at low use and red only near the limit', () => {
    render(<ContextRing fraction={0.2} />);
    expect(screen.getByRole('img').getAttribute('data-level')).toBe('ok');
    cleanup();
    render(<ContextRing fraction={0.95} />);
    expect(screen.getByRole('img').getAttribute('data-level')).toBe('high');
  });
});
