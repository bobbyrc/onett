// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SetupChecklist } from './SetupChecklist';

const state = vi.hoisted(() => ({
  owner: true,
  projects: [] as unknown[] | undefined,
  agents: [] as unknown[],
}));

vi.mock('../auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../auth')>()),
  useCan: () => state.owner,
}));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useProjects: () => ({ data: state.projects }),
  useAgents: () => ({ data: state.agents }),
  useStats: () => ({ data: { owner_unreachable: false } }),
  useSecrets: () => ({ data: { secrets: [] } }),
}));

afterEach(cleanup);

const ready = [{ name: 'claude', disabled: false, harness_cli_found: true }];
const project = { name: 'onett', policy: 'default' };

const show = (form: 'stations' | 'strip') =>
  render(
    <MemoryRouter>
      <SetupChecklist form={form} />
    </MemoryRouter>,
  );

describe('SetupChecklist stations (no project yet)', () => {
  it('shows five stations with the next one outlined and the policy step waiting', () => {
    state.owner = true;
    state.projects = [];
    state.agents = ready;
    show('stations');
    expect(screen.getByText('Step 1 · next')).toBeTruthy();
    const add = screen.getByRole('link', { name: 'Add a project' });
    expect(add.getAttribute('href')).toBe('/projects');
    expect(add.classList.contains('btn-primary')).toBe(true);
    expect(screen.getByText('Step 3 · waits for a project')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Choose a policy' })).toBeNull();
    expect(document.querySelectorAll('.ov-station')).toHaveLength(5);
  });

  it('renders nothing in the strip form', () => {
    state.owner = true;
    state.projects = [];
    state.agents = ready;
    const { container } = show('strip');
    expect(container.textContent).toBe('');
  });
});

describe('SetupChecklist strip (a project exists)', () => {
  it('shows one line with the step left, and opens to the five steps', () => {
    state.owner = true;
    state.projects = [project];
    state.agents = [];
    show('strip');
    expect(screen.getByText('4 of 5 done', { exact: false })).toBeTruthy();
    const left = screen.getByRole('link', { name: 'Agents ready' });
    expect(left.getAttribute('href')).toBe('/config?section=agents');
    expect(screen.getByText('(No agent is enabled.)', { exact: false })).toBeTruthy();
    const toggle = screen.getByRole('button', { name: 'Show steps ▾' });
    expect(document.querySelectorAll('#setup-steps li')).toHaveLength(0);
    fireEvent.click(toggle);
    expect(document.querySelectorAll('#setup-steps li')).toHaveLength(5);
  });

  it('renders nothing in the stations form', () => {
    state.owner = true;
    state.projects = [project];
    state.agents = [];
    const { container } = show('stations');
    expect(container.textContent).toBe('');
  });
});

describe('SetupChecklist for a non-owner', () => {
  it('shows nothing in either form', () => {
    state.owner = false;
    state.projects = [];
    state.agents = ready;
    const first = show('stations');
    expect(first.container.textContent).toBe('');
    first.unmount();
    const second = show('strip');
    expect(second.container.textContent).toBe('');
  });
});
