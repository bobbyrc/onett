// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import { NotFound, isNotFound } from './NotFound';

afterEach(cleanup);

describe('NotFound', () => {
  it.each([
    ['task', 'Task not found', 'Back to tasks', '/tasks'],
    ['run', 'Run not found', 'Back to runs', '/runs'],
    ['goal', 'Goal not found', 'Back to goals', '/goals'],
  ] as const)('gives a %s a heading, a reason and a way back', (what, heading, back, href) => {
    render(
      <MemoryRouter>
        <NotFound what={what} />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { level: 1, name: heading })).toBeTruthy();
    expect(screen.getByText(`No ${what} has this ID. It may have been deleted, or the link is wrong.`)).toBeTruthy();
    expect(screen.getByRole('link', { name: back }).getAttribute('href')).toBe(href);
  });
});

describe('isNotFound', () => {
  it('is true only for the API 404', () => {
    expect(isNotFound(new ApiError(404, 'task nope: not found'))).toBe(true);
    expect(isNotFound(new ApiError(500, 'boom'))).toBe(false);
    expect(isNotFound(new Error('task nope: not found'))).toBe(false);
    expect(isNotFound(null)).toBe(false);
  });
});
