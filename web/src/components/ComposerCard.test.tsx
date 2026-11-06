// @vitest-environment jsdom
import { createRef } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Composer } from './Composer';
import { JumpToLatest } from './JumpToLatest';
import type { AttachmentUploads } from './ComposerAttachments';

afterEach(cleanup);

const props = {
  textareaRef: createRef<HTMLTextAreaElement>(),
  draft: '',
  onDraft: () => {},
  onSend: () => {},
  disabled: false,
  sendDisabled: true,
  label: 'Message the orchestrator',
};

const uploads = (over: Partial<AttachmentUploads> = {}): AttachmentUploads => ({
  items: [],
  add: vi.fn(),
  remove: vi.fn(),
  clear: vi.fn(),
  uploading: false,
  ids: [],
  ...over,
});

describe('Composer card', () => {
  it('asks "Ask anything…" unless the page passes a placeholder', () => {
    const { rerender } = render(<Composer {...props} />);
    expect(screen.getByPlaceholderText('Ask anything…')).toBeTruthy();
    rerender(<Composer {...props} placeholder="Message the implementer…" />);
    expect(screen.getByPlaceholderText('Message the implementer…')).toBeTruthy();
  });

  it('shows the + only when files can be attached, and it opens the file input', () => {
    const { rerender } = render(<Composer {...props} />);
    expect(screen.queryByRole('button', { name: 'Attach files' })).toBeNull();
    rerender(<Composer {...props} attach={uploads()} />);
    const input = screen.getByTestId('attach-input') as HTMLInputElement;
    const click = vi.spyOn(input, 'click');
    fireEvent.click(screen.getByRole('button', { name: 'Attach files' }));
    expect(click).toHaveBeenCalledTimes(1);
  });

  it('keeps the pending chips inside the card, above the field', () => {
    const file = new File(['x'], 'notes.txt', { type: 'text/plain' });
    const { container } = render(
      <Composer {...props} attach={uploads({ items: [{ key: 'f1', file, status: 'done', progress: 1, id: 'a1' }], ids: ['a1'] })} />,
    );
    const form = container.querySelector('form.composer')!;
    const chips = form.querySelector('.att-chips')!;
    const field = form.querySelector('textarea')!;
    expect(chips.compareDocumentPosition(field) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('labels the icon-only Send button and dims it while disabled', () => {
    render(<Composer {...props} />);
    const send = screen.getByRole('button', { name: 'Send' }) as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    expect(send.textContent).toBe('');
  });
});

describe('JumpToLatest pill', () => {
  it('has a down-arrow icon, a label and the unread count', () => {
    const onClick = vi.fn();
    const { container } = render(<JumpToLatest unread={2} onClick={onClick} />);
    expect(container.querySelector('svg')).toBeTruthy();
    const button = screen.getByRole('button', { name: /Jump to latest/ });
    expect(button.textContent).toContain('2 new');
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
