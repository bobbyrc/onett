// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Verdict } from '../lib/verdict';

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useControl: () => ({ data: undefined }),
  useStats: () => ({ data: undefined }),
}));

import { BoardVerdict } from './BoardVerdict';

afterEach(cleanup);

describe('BoardVerdict', () => {
  const verdict: Verdict = { lamp: 'stop', head: 'Work is stopped on onett', lead: '2 tasks on onett are blocked:', rest: 'Start there.' };

  it('shows the headline, the bold lead, the rest of the sentence and the lamp', () => {
    const { container } = render(<BoardVerdict verdict={verdict} />);
    expect(screen.getByRole('heading', { name: 'Work is stopped on onett' })).toBeTruthy();
    expect(screen.getByText('2 tasks on onett are blocked:').tagName).toBe('B');
    expect(document.querySelector('.ov-verdict-text')?.textContent).toBe('2 tasks on onett are blocked: Start there.');
    expect(container.querySelector('.ov-verdict-head .lamp')?.classList.contains('lamp-stop')).toBe(true);
  });

  it('renders the line window beside the sentence', () => {
    render(<BoardVerdict verdict={verdict} />);
    expect(screen.getByText('Line')).toBeTruthy();
  });
});
