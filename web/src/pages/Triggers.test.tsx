// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Trigger, TriggerFiring } from '../types';
import type { TriggerStarter } from '../lib/triggerForm';
import { Triggers } from './Triggers';

// What each hook answers. Each test sets these; the mutation mocks are reset before every test.
const state = vi.hoisted(() => ({
  owner: true,
  triggers: { isSuccess: true, isPending: false, data: [] as Trigger[], error: null as unknown },
  firings: { isSuccess: true, isPending: false, data: [] as TriggerFiring[], error: null as unknown },
  projects: [{ name: 'onett', repo: 'bobbyrc/onett' }] as { name: string; repo?: string }[],
  run: { mutate: vi.fn(), isPending: false, error: null as unknown, variables: undefined as string | undefined },
  patch: {
    mutate: vi.fn(),
    isPending: false,
    error: null as unknown,
    variables: undefined as { id: string; patch: unknown } | undefined,
  },
}));

vi.mock('../triggerQueries', () => ({
  useTriggers: () => state.triggers,
  useAllTriggerFirings: () => state.firings,
  useRunTrigger: () => state.run,
  usePatchTrigger: () => state.patch,
}));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useProjects: () => ({ isSuccess: true, data: state.projects, error: null }),
}));

vi.mock('../auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../auth')>()),
  useCan: () => state.owner,
}));

vi.mock('../components/TriggerDialog', () => ({
  TriggerDialog: ({ trigger, starter }: { trigger?: Trigger; starter?: TriggerStarter }) => (
    <div data-testid="dialog">{trigger?.name ?? starter?.prompt ?? 'new'}</div>
  ),
}));

const now = new Date().toISOString();

const zeta: Trigger = {
  id: 't-zeta',
  name: 'Zeta sweep',
  kind: 'schedule',
  project: 'onett',
  prompt: 'Sweep the backlog.',
  enabled: true,
  cron: '0 5 * * *',
  time_zone: 'UTC',
  next_run_at: '2999-01-01T05:00:00Z',
  created_at: now,
  updated_at: now,
  last_firing: {
    id: 'f-zeta',
    trigger_id: 't-zeta',
    at: now,
    cause: 'schedule',
    outcome: 'started',
    conversation_id: 'c1',
  },
};

const alpha: Trigger = {
  id: 't-alpha',
  name: 'Alpha issues',
  kind: 'github_issue',
  project: 'onett',
  label: 'bug',
  prompt: 'Look at the new issue.',
  enabled: true,
  created_at: now,
  updated_at: now,
  last_firing: {
    id: 'f-alpha',
    trigger_id: 't-alpha',
    at: now,
    cause: 'github_issue',
    issue_number: 87,
    outcome: 'failed',
    reason: 'onett was shutting down',
  },
};

const beta: Trigger = {
  id: 't-beta',
  name: 'Beta nudge',
  kind: 'schedule',
  project: 'onett',
  prompt: 'Nudge the team.',
  enabled: false,
  cron: '0 */6 * * *',
  time_zone: 'UTC',
  created_at: now,
  updated_at: now,
};

const logFirings: TriggerFiring[] = [
  { id: 'L1', trigger_id: 't-zeta', at: now, cause: 'schedule', outcome: 'started', conversation_id: 'c1' },
  { id: 'L2', trigger_id: 't-beta', at: now, cause: 'schedule', outcome: 'skipped', reason: 'the factory was paused' },
  {
    id: 'L3',
    trigger_id: 't-alpha',
    at: now,
    cause: 'github_issue',
    outcome: 'started',
    issue_number: 88,
    issue_url: 'https://github.com/bobbyrc/onett/issues/88',
  },
];

beforeEach(() => {
  state.owner = true;
  state.triggers = { isSuccess: true, isPending: false, data: [zeta, alpha, beta], error: null };
  state.firings = { isSuccess: true, isPending: false, data: logFirings, error: null };
  state.projects = [{ name: 'onett', repo: 'bobbyrc/onett' }];
  state.run = { mutate: vi.fn(), isPending: false, error: null, variables: undefined };
  state.patch = { mutate: vi.fn(), isPending: false, error: null, variables: undefined };
});

afterEach(cleanup);

const renderPage = (path = '/triggers') =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Triggers />
    </MemoryRouter>,
  );

const rulesRegion = () => screen.getByRole('region', { name: /^Rules/ });
const logRegion = () => screen.getByRole('region', { name: /^Recent firings/ });

describe('Triggers list', () => {
  it('lists the cards in order, with their links, summary, actions and words', () => {
    renderPage();
    const headings = within(rulesRegion())
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.querySelector('a')?.textContent);
    expect(headings).toEqual(['Alpha issues', 'Zeta sweep', 'Beta nudge']);

    expect(screen.getByRole('link', { name: 'Zeta sweep' }).getAttribute('href')).toBe('/triggers/t-zeta');
    expect(rulesRegion().querySelector('.section-note')?.textContent).toBe('2 on · 1 off · 1 failing');

    expect(screen.getByRole('button', { name: 'Run Zeta sweep now' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Run Alpha issues now' })).toBeNull();

    expect(screen.getByText('A new issue in bobbyrc/onett')).toBeTruthy();
    expect(screen.getByText(/label bug/)).toBeTruthy();
    expect(screen.getByText('Every day at 05:00')).toBeTruthy();
  });

  it('shows no controls to a non-owner, only the note', () => {
    state.owner = false;
    renderPage();
    expect(screen.getByText('Only an owner can add or change triggers.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'New trigger' })).toBeNull();
    expect(screen.queryAllByRole('button', { name: /^Run .* now$/ })).toHaveLength(0);
    expect(screen.queryAllByRole('button', { name: /^Edit / })).toHaveLength(0);
    expect(screen.queryAllByRole('button', { name: /^Turn (on|off) / })).toHaveLength(0);
  });

  it('with no triggers, offers two examples to an owner and the empty text', () => {
    state.triggers = { isSuccess: true, isPending: false, data: [], error: null };
    state.firings = { isSuccess: true, isPending: false, data: [], error: null };
    renderPage();
    expect(
      screen.getByText(
        'No triggers. A trigger starts the orchestrator on its own, on a schedule or when a new issue is opened.',
      ),
    ).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /^Use this example/ })).toHaveLength(2);
    expect(
      screen.getByText('Nothing has fired yet. Each firing is listed here with what started it and what came of it.'),
    ).toBeTruthy();

    fireEvent.click(screen.getAllByRole('button', { name: /^Use this example/ })[0]);
    expect(screen.getByTestId('dialog').textContent).toBe('Address any open issues in this repository.');
  });

  it('with no triggers, shows the empty text and no examples to a non-owner', () => {
    state.owner = false;
    state.triggers = { isSuccess: true, isPending: false, data: [], error: null };
    renderPage();
    expect(screen.getByText(/^No triggers\./)).toBeTruthy();
    expect(screen.queryAllByRole('button', { name: /Use this/ })).toHaveLength(0);
  });

  it('opens the dialog for a new trigger and for an edit', () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: 'New trigger' }));
    expect(screen.getByTestId('dialog').textContent).toBe('new');

    fireEvent.click(screen.getByRole('button', { name: 'Edit Zeta sweep' }));
    expect(screen.getByTestId('dialog').textContent).toBe('Zeta sweep');
  });

  it('runs a schedule trigger now and reports what it fired', () => {
    state.run.mutate.mockImplementation((_id: string, opts: { onSuccess: (f: TriggerFiring) => void }) =>
      opts.onSuccess(logFirings[0]),
    );
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: 'Run Zeta sweep now' }));
    expect(state.run.mutate).toHaveBeenCalledWith('t-zeta', expect.any(Object));
    expect(screen.getByRole('status').textContent).toBe('Fired Zeta sweep.');
  });

  it('turns a trigger off and on through the patch', () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: 'Turn off Zeta sweep' }));
    expect(state.patch.mutate).toHaveBeenCalledWith({ id: 't-zeta', patch: { enabled: false } });

    fireEvent.click(screen.getByRole('button', { name: 'Turn on Beta nudge' }));
    expect(state.patch.mutate).toHaveBeenCalledWith({ id: 't-beta', patch: { enabled: true } });
  });

  it('shows the firings log grouped under today, with names, issue link, reason and conversation', () => {
    renderPage();
    expect(screen.getByRole('heading', { level: 3, name: 'Today' })).toBeTruthy();

    const log = within(logRegion());
    expect(log.getByText('Zeta sweep')).toBeTruthy();
    expect(log.getByText('Alpha issues')).toBeTruthy();
    expect(log.getByRole('link', { name: 'issue #88' }).getAttribute('href')).toBe(
      'https://github.com/bobbyrc/onett/issues/88',
    );
    expect(log.getByText('the factory was paused')).toBeTruthy();
    expect(log.getByRole('link', { name: 'Open conversation' }).getAttribute('href')).toBe('/chat/c1');
  });

  it('on a phone, the Firings tab shows the log alone and is selected', () => {
    const { container } = renderPage();
    const firingsTab = screen.getByRole('tab', { name: 'Firings' });
    fireEvent.click(firingsTab);
    expect(container.querySelector('.trg-cols')?.classList.contains('tab-firings')).toBe(true);
    expect(firingsTab.getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('tab', { name: 'Triggers' }).getAttribute('aria-selected')).toBe('false');
  });
});
