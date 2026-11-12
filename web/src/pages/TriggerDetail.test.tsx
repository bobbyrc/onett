// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api';
import type { Trigger, TriggerFiring } from '../types';
import { TriggerDetail } from './TriggerDetail';

// What each hook answers; the tests set these, and the mutation mocks are rebuilt before each test.
const mocks = vi.hoisted(() => ({
  detail: {} as unknown,
  firings: {} as unknown,
  run: {} as Record<string, unknown>,
  patch: {} as Record<string, unknown>,
  remove: {} as Record<string, unknown>,
  can: true,
}));

vi.mock('../triggerQueries', () => ({
  useTrigger: () => mocks.detail,
  useTriggerFirings: () => mocks.firings,
  useRunTrigger: () => mocks.run,
  usePatchTrigger: () => mocks.patch,
  useDeleteTrigger: () => mocks.remove,
}));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useProjects: () => ({ data: [{ name: 'onett', repo: 'bobbyrc/onett' }] }),
}));

vi.mock('../auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../auth')>()),
  useCan: () => mocks.can,
}));

vi.mock('../components/TriggerDialog', () => ({
  TriggerDialog: ({ trigger }: { trigger?: Trigger }) => <div data-testid="dialog">{trigger?.name}</div>,
}));

const schedule: Trigger = {
  id: 't1',
  name: 'Morning issue sweep',
  kind: 'schedule',
  project: 'onett',
  prompt: 'Look at every open issue in this repository.',
  enabled: true,
  cron: '0 5 * * *',
  time_zone: 'UTC',
  next_run_at: '2026-11-13T05:00:00Z',
  conversation_id: 'c1',
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-12T05:00:00Z',
  last_firing: {
    id: 'f0',
    trigger_id: 't1',
    at: '2026-11-12T05:00:00Z',
    cause: 'schedule',
    scheduled_for: '2026-11-12T05:00:00Z',
    outcome: 'started',
  },
};

const issueTrigger: Trigger = {
  id: 't2',
  name: 'Bug reports',
  kind: 'github_issue',
  project: 'onett',
  prompt: 'Triage the new bug report.',
  enabled: true,
  label: 'bug',
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-12T05:00:00Z',
};

const firingsOf = (...list: TriggerFiring[]) => ({ isSuccess: true, isError: false, error: null, data: list });

const scheduledStarted: TriggerFiring = {
  id: 'f1',
  trigger_id: 't1',
  at: '2026-11-12T05:00:00Z',
  cause: 'schedule',
  scheduled_for: '2026-11-12T05:00:00Z',
  outcome: 'started',
};
const manualStarted: TriggerFiring = {
  id: 'f2',
  trigger_id: 't1',
  at: '2026-11-11T14:12:00Z',
  cause: 'manual',
  by: 'owner:bcraig',
  outcome: 'started',
};
const scheduledSkipped: TriggerFiring = {
  id: 'f3',
  trigger_id: 't1',
  at: '2026-11-11T05:00:00Z',
  cause: 'schedule',
  scheduled_for: '2026-11-11T05:00:00Z',
  outcome: 'skipped',
  reason: 'the factory was paused',
};

function mutation() {
  return { mutate: vi.fn(), isPending: false, error: null };
}

beforeEach(() => {
  mocks.detail = { isError: false, error: null, data: schedule };
  mocks.firings = firingsOf();
  mocks.run = mutation();
  mocks.patch = mutation();
  mocks.remove = mutation();
  mocks.can = true;
});

afterEach(cleanup);

function renderAt(path = '/triggers/t1') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/triggers/:id" element={<TriggerDetail />} />
        <Route path="/triggers" element={<p>LIST</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('TriggerDetail: a schedule trigger', () => {
  it('shows the name, its status, the facts, the prompt and the owner actions', () => {
    renderAt();
    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Morning issue sweep');
    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('OK');
    expect(screen.getByText('Every day at 05:00')).toBeTruthy();
    expect(screen.getByText('UTC')).toBeTruthy();
    expect(screen.getByText('0 5 * * *')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open conversation' }).getAttribute('href')).toBe('/chat/c1');
    expect(screen.getByText('Look at every open issue in this repository.')).toBeTruthy();
    for (const name of ['Run now', 'Edit', 'Turn off', 'Delete']) {
      expect(screen.getByRole('button', { name })).toBeTruthy();
    }
  });
});

describe('TriggerDetail: firings', () => {
  it('lists each firing with its time, cause, outcome and reason', () => {
    mocks.firings = firingsOf(scheduledStarted, manualStarted, scheduledSkipped);
    renderAt();
    expect(screen.getAllByText('Scheduled for 05:00').length).toBe(2);
    expect(screen.getByText('Run now by bcraig')).toBeTruthy();
    expect(screen.getByText('skipped')).toBeTruthy();
    expect(screen.getByText('the factory was paused')).toBeTruthy();
  });

  it('says so when the trigger has not fired', () => {
    renderAt();
    expect(screen.getByText('This trigger has not fired yet.')).toBeTruthy();
  });
});

describe('TriggerDetail: a GitHub issue trigger', () => {
  it('has no Run now, shows the issue rule, and links each issue', () => {
    mocks.detail = { isError: false, error: null, data: issueTrigger };
    mocks.firings = firingsOf({
      id: 'f9',
      trigger_id: 't2',
      at: '2026-11-12T05:00:00Z',
      cause: 'github_issue',
      issue_number: 88,
      issue_url: 'https://github.com/bobbyrc/onett/issues/88',
      outcome: 'started',
    });
    renderAt('/triggers/t2');
    expect(screen.queryByRole('button', { name: 'Run now' })).toBeNull();
    expect(screen.getByText('When an issue is opened')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'bobbyrc/onett' }).getAttribute('href')).toBe('https://github.com/bobbyrc/onett');
    expect(screen.getByText('bug')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Issue #88' }).getAttribute('href')).toBe('https://github.com/bobbyrc/onett/issues/88');
  });
});

describe('TriggerDetail: a viewer who is not an owner', () => {
  it('gets no actions, but still sees the firings and the conversation link', () => {
    mocks.can = false;
    mocks.firings = firingsOf(scheduledStarted);
    renderAt();
    for (const name of ['Run now', 'Edit', 'Turn off', 'Delete']) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
    expect(screen.getAllByText('Scheduled for 05:00').length).toBe(1);
    expect(screen.getByRole('link', { name: 'Open conversation' })).toBeTruthy();
  });
});

describe('TriggerDetail: actions', () => {
  it('Run now runs the trigger and says what the firing came to', () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: 'Run now' }));
    expect(mocks.run.mutate).toHaveBeenCalledWith('t1', expect.objectContaining({ onSuccess: expect.any(Function) }));

    const mutate = mocks.run.mutate as ReturnType<typeof vi.fn>;
    mutate.mockImplementation((_id: string, opts: { onSuccess: (f: TriggerFiring) => void }) =>
      opts.onSuccess({ ...manualStarted, outcome: 'skipped', reason: 'the factory is paused' }),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Run now' }));
    expect(screen.getByRole('status').textContent).toBe('Skipped Morning issue sweep: the factory is paused.');
  });

  it('Turn off patches the trigger to disabled', () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: 'Turn off' }));
    expect(mocks.patch.mutate).toHaveBeenCalledWith({ id: 't1', patch: { enabled: false } });
  });

  it('Edit opens the dialog for this trigger', () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(screen.getByTestId('dialog').textContent).toBe('Morning issue sweep');
  });

  it('Delete asks first, then deletes and goes back to the list', () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(mocks.remove.mutate).not.toHaveBeenCalled();
    expect(screen.getByText('Deletes the trigger and its firing history. Its conversation stays in Chat.')).toBeTruthy();

    const mutate = mocks.remove.mutate as ReturnType<typeof vi.fn>;
    mutate.mockImplementation((_id: string, opts: { onSuccess: () => void }) => opts.onSuccess());
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }));
    expect(mutate).toHaveBeenCalledWith('t1', expect.any(Object));
    expect(screen.getByText('LIST')).toBeTruthy();
  });
});

describe('TriggerDetail: errors', () => {
  it('an unknown trigger gets a heading and a way back to the list', () => {
    mocks.detail = { isError: true, error: new ApiError(404, 'trigger nope: not found'), data: undefined };
    renderAt('/triggers/nope');
    expect(screen.getByRole('heading', { level: 1, name: 'Trigger not found' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Back to triggers' }).getAttribute('href')).toBe('/triggers');
    expect(screen.queryByText('trigger nope: not found')).toBeNull();
  });

  it('any other error shows the error text in an alert', () => {
    mocks.detail = { isError: true, error: new ApiError(500, 'boom'), data: undefined };
    renderAt('/triggers/t1');
    expect(screen.getByRole('alert').textContent).toContain('boom');
  });
});
