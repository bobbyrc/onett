// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { BlockedReason } from './BlockedReason';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

// jsdom has no layout, so stand in for the browser's measurement.
function overflowing(on: boolean) {
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(on ? 200 : 40);
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(60);
}

describe('BlockedReason', () => {
  it('has no toggle for a short reason', () => {
    overflowing(false);
    render(<BlockedReason>Denied.</BlockedReason>);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('clamps a long reason and toggles it open and closed', () => {
    overflowing(true);
    const { container } = render(<BlockedReason title="Blocked">{'line\n'.repeat(15)}</BlockedReason>);
    expect(container.querySelector('.is-clamped')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Show more' }));
    expect(container.querySelector('.is-clamped')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Show less' }));
    expect(container.querySelector('.is-clamped')).not.toBeNull();
  });
});
