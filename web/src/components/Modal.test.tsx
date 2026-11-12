// @vitest-environment jsdom
import { useState, type ReactNode } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Modal } from './Modal';

afterEach(cleanup);

/** A page with an opener button, a link behind the dialog, and the dialog itself. */
function Harness({ body }: { body?: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open
      </button>
      <a href="/elsewhere">Behind</a>
      <Modal open={open} title="Rename" onClose={() => setOpen(false)}>
        {body ?? (
          <>
            <input aria-label="Name" />
            <button type="button">Save</button>
          </>
        )}
      </Modal>
    </>
  );
}

function openDialog(body?: ReactNode) {
  render(<Harness body={body} />);
  const opener = screen.getByRole('button', { name: 'Open' });
  opener.focus();
  fireEvent.click(opener);
  return opener;
}

/** Presses Tab on whatever has focus. Returns false when the dialog handled it itself. */
const tab = (shiftKey = false) => fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Tab', shiftKey });

describe('Modal focus', () => {
  it('moves focus to the first control of the body when it opens', () => {
    openDialog();
    expect(screen.getByLabelText('Name')).toHaveFocus();
  });

  it('leaves focus on a field that asked for it with autoFocus', () => {
    openDialog(
      <>
        <input aria-label="First" />
        <input aria-label="Second" autoFocus />
      </>,
    );
    expect(screen.getByLabelText('Second')).toHaveFocus();
  });

  it('focuses the dialog itself when the body has nothing to focus', () => {
    openDialog(<p>Only text</p>);
    expect(screen.getByRole('dialog')).toHaveFocus();
  });

  it('keeps Tab inside: it wraps from the last control to the first and back', () => {
    openDialog();
    const close = screen.getByRole('button', { name: 'Close' });
    const save = screen.getByRole('button', { name: 'Save' });

    save.focus();
    expect(tab()).toBe(false);
    expect(close).toHaveFocus();

    expect(tab(true)).toBe(false);
    expect(save).toHaveFocus();
  });

  it('lets Tab move normally between controls in the middle of the dialog', () => {
    openDialog();
    screen.getByLabelText('Name').focus();
    // Not prevented: the browser moves focus to the next control by itself.
    expect(tab()).toBe(true);
  });

  it('pulls focus back in when Tab is pressed with focus outside the dialog', () => {
    openDialog();
    screen.getByRole('link', { name: 'Behind' }).focus();
    expect(tab()).toBe(false);
    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus();
  });

  it('returns focus to the opener when it closes', () => {
    const opener = openDialog();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(opener).toHaveFocus();
  });
});
