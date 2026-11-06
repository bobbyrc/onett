// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { Composer } from './Composer';

const props = {
  textareaRef: createRef<HTMLTextAreaElement>(),
  draft: '',
  onDraft: vi.fn(),
  onSend: vi.fn(),
  disabled: false,
  sendDisabled: true,
  label: 'Message',
};

afterEach(cleanup);

describe('Composer model label', () => {
  it('shows the label in the footer only when given', () => {
    const { rerender } = render(<Composer {...props} />);
    expect(screen.queryByText('implementer · claude-sonnet')).toBeNull();
    rerender(<Composer {...props} modelLabel="implementer · claude-sonnet" />);
    expect(screen.getByText('implementer · claude-sonnet')).toBeTruthy();
  });
});
