// @vitest-environment jsdom
// Pins the component test setup: render, query, jest-dom matchers and automatic cleanup.
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

describe('component test setup', () => {
  it('renders a component and matches it with jest-dom', () => {
    render(<button disabled>Save</button>);
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  });

  it('unmounts the previous test between tests', () => {
    expect(screen.queryByRole('button')).toBeNull();
  });
});
