// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { SecretsPanel } from './SecretsPanel';

const loaded = { data: [], isSuccess: true, isPending: false, isError: false, error: null };

vi.mock('../../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../queries')>()),
  useSecrets: () => loaded,
  usePutSecret: () => ({ mutate: () => {} }),
  useDeleteSecret: () => ({ mutate: () => {} }),
}));

afterEach(cleanup);

describe('SecretsPanel text', () => {
  it('says a saved secret applies at once, not that it needs a restart', () => {
    render(
      <MemoryRouter>
        <SecretsPanel />
      </MemoryRouter>,
    );
    const text = document.body.textContent ?? '';
    expect(text).toContain('A saved secret applies at once, including to chat bots and notifiers.');
    expect(text).not.toMatch(/restart/i);
  });
});
