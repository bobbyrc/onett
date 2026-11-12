// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Trigger } from '../types';
import { TriggerDialog } from './TriggerDialog';

// The two mutations, as the dialog sees them. Each test sets the error it wants.
const mocks = vi.hoisted(() => ({
  create: { mutate: vi.fn(), isPending: false, error: null as unknown },
  patch: { mutate: vi.fn(), isPending: false, error: null as unknown },
}));

vi.mock('../triggerQueries', () => ({
  useCreateTrigger: () => mocks.create,
  usePatchTrigger: () => mocks.patch,
}));

vi.mock('../queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../queries')>()),
  useProjects: () => ({
    data: [
      { name: 'onett', repo: 'bobbyrc/onett' },
      { name: 'home-lab', repo: 'bobbyrc/home-lab' },
    ],
  }),
}));

beforeEach(() => {
  mocks.create = { mutate: vi.fn(), isPending: false, error: null };
  mocks.patch = { mutate: vi.fn(), isPending: false, error: null };
});

afterEach(cleanup);

function trigger(over: Partial<Trigger> = {}): Trigger {
  return {
    id: 't1',
    name: 'Nightly',
    kind: 'schedule',
    project: 'home-lab',
    prompt: 'Do it.',
    enabled: true,
    cron: '30 9 * * 1-5',
    time_zone: 'America/New_York',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
    ...over,
  };
}

function fillNew(name = 'Nightly', prompt = 'Do it.') {
  fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: name } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Prompt' }), { target: { value: prompt } });
}

describe('TriggerDialog: a new trigger', () => {
  it('shows the fields of a new schedule trigger', () => {
    render(<TriggerDialog onClose={() => {}} />);
    expect(screen.getByRole('dialog', { name: 'New trigger' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Name' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Project' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'On a schedule' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByRole('button', { name: 'On a new issue' }).getAttribute('aria-pressed')).toBe('false');
    expect(screen.getByRole('combobox', { name: 'Repeats' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'At' })).toHaveValue('09:00');
    expect(screen.getByRole('combobox', { name: 'Time zone' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Prompt' })).toBeTruthy();
    expect(screen.getByRole('checkbox', { name: /^On/ })).toBeChecked();
  });

  it('shows the chosen days and says the schedule back', () => {
    render(<TriggerDialog onClose={() => {}} />);
    fireEvent.change(screen.getByRole('combobox', { name: 'Repeats' }), { target: { value: 'days' } });
    const days = within(screen.getByRole('group', { name: 'Days' })).getAllByRole('button');
    expect(days).toHaveLength(7);
    for (const name of ['Mon', 'Tue', 'Wed', 'Thu', 'Fri']) {
      expect(screen.getByRole('button', { name }).getAttribute('aria-pressed')).toBe('true');
    }
    for (const name of ['Sat', 'Sun']) {
      expect(screen.getByRole('button', { name }).getAttribute('aria-pressed')).toBe('false');
    }
    expect(screen.getByRole('note').textContent).toMatch(/^Fires Monday to Friday at 09:00, /);
  });

  it('drops Repeats and asks for a label when the trigger starts on a new issue', () => {
    render(<TriggerDialog onClose={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: 'On a new issue' }));
    expect(screen.queryByRole('combobox', { name: 'Repeats' })).toBeNull();
    expect(screen.getByRole('textbox', { name: 'Label' })).toBeTruthy();
    expect(screen.getByRole('note').textContent).toContain('in bobbyrc/onett.');

    fireEvent.change(screen.getByRole('textbox', { name: 'Label' }), { target: { value: 'bug' } });
    expect(screen.getByRole('note').textContent).toContain('labelled bug in bobbyrc/onett.');
  });

  it('asks for a name and does not create anything without one', () => {
    render(<TriggerDialog onClose={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: 'Create trigger' }));
    expect(screen.getByRole('alert').textContent).toBe('A name is required.');
    expect(mocks.create.mutate).not.toHaveBeenCalled();
  });

  it('creates a schedule trigger in the first project, enabled, at 09:00 every day', () => {
    const onClose = vi.fn();
    render(<TriggerDialog onClose={onClose} />);
    fillNew();
    fireEvent.click(screen.getByRole('button', { name: 'Create trigger' }));
    expect(mocks.create.mutate).toHaveBeenCalledTimes(1);
    const [body] = mocks.create.mutate.mock.calls[0];
    expect(body).toMatchObject({
      name: 'Nightly',
      kind: 'schedule',
      project: 'onett',
      prompt: 'Do it.',
      enabled: true,
      cron: '0 9 * * *',
    });
  });

  it('shows a server error in an alert', () => {
    mocks.create.error = new Error('bad cron');
    render(<TriggerDialog onClose={() => {}} />);
    expect(screen.getByRole('alert').textContent).toContain('bad cron');
  });

  it('shows the cron of the form as an editable expression when Custom is chosen', () => {
    render(<TriggerDialog onClose={() => {}} />);
    fireEvent.change(screen.getByRole('combobox', { name: 'Repeats' }), { target: { value: 'custom' } });
    expect(screen.getByRole('textbox', { name: 'Cron expression' })).toHaveValue('0 9 * * *');
    expect(screen.queryByText(/Saved as cron/)).toBeNull();
  });
});

describe('TriggerDialog: an existing trigger', () => {
  it('shows the kind and project as fixed, with no way to change them', () => {
    render(<TriggerDialog trigger={trigger()} onClose={() => {}} />);
    expect(screen.getByRole('dialog', { name: 'Edit trigger' })).toBeTruthy();
    expect(screen.queryByRole('combobox', { name: 'Project' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'On a schedule' })).toBeNull();
    expect(
      screen.getByText(
        'Schedule trigger for home-lab. The kind and the project cannot be changed; make a new trigger for another one.',
      ),
    ).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Repeats' })).toHaveValue('days');
  });

  it('sends nothing and closes when saved without a change', () => {
    const onClose = vi.fn();
    render(<TriggerDialog trigger={trigger()} onClose={onClose} />);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(mocks.patch.mutate).not.toHaveBeenCalled();
  });

  it('sends only the changed name', () => {
    render(<TriggerDialog trigger={trigger()} onClose={() => {}} />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'New name' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(mocks.patch.mutate).toHaveBeenCalledTimes(1);
    expect(mocks.patch.mutate.mock.calls[0][0]).toEqual({ id: 't1', patch: { name: 'New name' } });
  });

  it('sends only what this dialog changed, not a switch-off made while it was open', () => {
    const { rerender } = render(<TriggerDialog trigger={trigger()} onClose={() => {}} />);
    rerender(<TriggerDialog trigger={trigger({ enabled: false })} onClose={() => {}} />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'New name' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(mocks.patch.mutate).toHaveBeenCalledTimes(1);
    expect(mocks.patch.mutate.mock.calls[0][0]).toEqual({ id: 't1', patch: { name: 'New name' } });
  });
});
