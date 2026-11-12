// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { OwnerUnreachableBanner } from './OwnerUnreachableBanner';

const role = vi.hoisted(() => ({ owner: true }));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useStats: () => ({ data: { owner_unreachable: true, blocked_goals: [] } }),
}));

vi.mock('../auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../auth')>()),
  useCan: () => role.owner,
}));

afterEach(cleanup);

function renderBanner() {
  return render(
    <MemoryRouter>
      <OwnerUnreachableBanner />
    </MemoryRouter>,
  );
}

describe('OwnerUnreachableBanner', () => {
  it('links an owner to channel setup', () => {
    role.owner = true;
    renderBanner();
    expect(screen.getByRole('link', { name: 'Set up a channel or notifier' }).getAttribute('href')).toBe('/config?mode=setup');
    expect(document.body.textContent).not.toContain('docs/channels.md');
  });

  it('tells a non-owner who can set up a channel, without a link', () => {
    role.owner = false;
    renderBanner();
    expect(screen.queryByRole('link', { name: 'Set up a channel or notifier' })).toBeNull();
    expect(
      screen.getByText('An owner can set up a channel or notifier (Telegram, Discord or ntfy) in Config.', { exact: false }),
    ).toBeTruthy();
    expect(document.body.textContent).not.toContain('docs/channels.md');
  });
});
