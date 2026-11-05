// @vitest-environment jsdom
import { createRef } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Composer } from './Composer';

afterEach(cleanup);

function setup(props: Partial<React.ComponentProps<typeof Composer>> = {}) {
  const onSend = vi.fn();
  const onDraft = vi.fn();
  const utils = render(
    <Composer
      textareaRef={createRef()}
      draft=""
      onDraft={onDraft}
      onSend={onSend}
      disabled={false}
      sendDisabled={false}
      placeholder="Message"
      label="Message the orchestrator"
      {...props}
    />,
  );
  return { onSend, onDraft, ...utils };
}

const field = () => screen.getByLabelText('Message the orchestrator');

describe('Composer Send and Stop', () => {
  it('shows Send when idle', () => {
    setup({ draft: 'hi' });
    expect(screen.getByRole('button', { name: 'Send' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull();
  });

  it('turns into Stop while a turn runs with an empty field, and back to Send', () => {
    const onStop = vi.fn();
    const { rerender, onSend } = setup({ onStop });
    const stop = screen.getByRole('button', { name: 'Stop' });
    fireEvent.click(stop);
    expect(onStop).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();

    // Typing while the turn runs offers Send (queues the message) instead.
    rerender(
      <Composer
        textareaRef={createRef()}
        draft="next"
        onDraft={() => {}}
        onSend={onSend}
        disabled={false}
        sendDisabled={false}
        placeholder="Message"
        label="Message the orchestrator"
        onStop={onStop}
      />,
    );
    expect(screen.getByRole('button', { name: 'Send' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull();

    // The run ended: no onStop, Send again.
    rerender(
      <Composer
        textareaRef={createRef()}
        draft=""
        onDraft={() => {}}
        onSend={onSend}
        disabled={false}
        sendDisabled
        placeholder="Message"
        label="Message the orchestrator"
      />,
    );
    expect(screen.getByRole('button', { name: 'Send' })).toBeTruthy();
  });

  it('shows Stopping… disabled and busy after Stop is pressed', () => {
    setup({ onStop: vi.fn(), stopping: true });
    const button = screen.getByRole('button', { name: 'Stopping…' }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.getAttribute('aria-busy')).toBe('true');
  });

  it('Esc stops a running turn once', () => {
    const onStop = vi.fn();
    setup({ onStop });
    fireEvent.keyDown(field(), { key: 'Escape' });
    expect(onStop).toHaveBeenCalledTimes(1);
  });
});

describe('Composer keys', () => {
  it('Enter sends and Shift+Enter does not', () => {
    const { onSend } = setup({ draft: 'hi' });
    fireEvent.keyDown(field(), { key: 'Enter', shiftKey: true });
    expect(onSend).not.toHaveBeenCalled();
    fireEvent.keyDown(field(), { key: 'Enter' });
    expect(onSend).toHaveBeenCalledTimes(1);
  });

  it('does not send while an IME composition is active', () => {
    const { onSend } = setup({ draft: 'こん' });
    fireEvent.keyDown(field(), { key: 'Enter', isComposing: true });
    fireEvent.keyDown(field(), { key: 'Enter', keyCode: 229 });
    expect(onSend).not.toHaveBeenCalled();
  });
});
