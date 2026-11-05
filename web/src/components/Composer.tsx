import './Composer.css';
import { useLayoutEffect, useRef, useState, type ChangeEvent, type DragEvent, type RefObject } from 'react';
import { cx } from '../lib/format';
import type { composerState } from '../lib/chatState';
import type { StickToBottom } from '../lib/useStickToBottom';
import { JumpToLatest } from './JumpToLatest';
import { PendingChips, type AttachmentUploads } from './ComposerAttachments';

export interface ComposerProps {
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  draft: string;
  onDraft: (value: string) => void;
  onSend: () => void;
  disabled: boolean;
  sendDisabled: boolean;
  placeholder: string;
  label: string;
  /** A turn is running and can be stopped: Stop takes Send's place while the field is empty, and Esc stops. */
  onStop?: () => void;
  /** Stop was pressed and the run has not ended yet. */
  stopping?: boolean;
  hint?: string;
  autoFocus?: boolean;
  /** Files for this message; without it the composer takes text only. */
  attach?: AttachmentUploads;
}

function SendIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <path d="M8 13V3M3.5 7.5 8 3l4.5 4.5" fill="none" stroke="currentColor" strokeWidth="1.75" />
    </svg>
  );
}

function StopIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <rect x="3.5" y="3.5" width="9" height="9" fill="currentColor" />
    </svg>
  );
}

/** The message sheet: an auto-growing field, with chips above and the hint and Send/Stop below. */
export function Composer({
  textareaRef,
  draft,
  onDraft,
  onSend,
  disabled,
  sendDisabled,
  placeholder,
  label,
  onStop,
  stopping = false,
  hint = 'Enter to send · Shift+Enter for a new line',
  autoFocus = false,
  attach,
}: ComposerProps) {
  const fileRef = useRef<HTMLInputElement>(null);
  const photoRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);
  // Phones get a second button that opens the camera and photo library directly.
  const touch = typeof window !== 'undefined' && !!window.matchMedia?.('(pointer: coarse)').matches;
  const pick = (event: ChangeEvent<HTMLInputElement>) => {
    attach?.add(Array.from(event.target.files ?? []));
    event.target.value = ''; // so picking the same file again fires a change
  };
  const hasFiles = (event: DragEvent) => Array.from(event.dataTransfer?.types ?? []).includes('Files');
  // Grow the field with its content; the CSS max-height (40vh) caps it and the field scrolls inside.
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  }, [draft, textareaRef]);

  // While a turn runs and there is nothing to send, the button stops it. Typing a message (or
  // adding a file) turns it back into Send, which queues the message behind the running turn.
  const hasContent = draft.trim() !== '' || !!attach?.ids.length || !!attach?.uploading;
  const stops = !!onStop && !hasContent;
  const buttonLabel = stops ? (stopping ? 'Stopping…' : 'Stop') : 'Send';

  return (
    <form
      className={cx('composer', dragging && 'is-dragging')}
      onSubmit={(event) => {
        event.preventDefault();
        if (!stops) onSend();
      }}
      onDragEnter={
        attach
          ? (event) => {
              if (!hasFiles(event)) return;
              dragDepth.current++;
              setDragging(true);
            }
          : undefined
      }
      onDragOver={attach ? (event) => hasFiles(event) && event.preventDefault() : undefined}
      onDragLeave={
        attach
          ? (event) => {
              if (!hasFiles(event)) return;
              dragDepth.current = Math.max(0, dragDepth.current - 1);
              if (dragDepth.current === 0) setDragging(false);
            }
          : undefined
      }
      onDrop={
        attach
          ? (event) => {
              if (!hasFiles(event)) return;
              event.preventDefault();
              dragDepth.current = 0;
              setDragging(false);
              attach.add(Array.from(event.dataTransfer.files));
            }
          : undefined
      }
    >
      {dragging ? <div className="composer-drop" aria-hidden="true">Drop files to attach</div> : null}
      {attach ? <PendingChips uploads={attach} /> : null}
      <label className="sr-only" htmlFor="composer">
        {label}
      </label>
      <textarea
        id="composer"
        ref={textareaRef}
        rows={1}
        autoFocus={autoFocus}
        value={draft}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(event) => onDraft(event.target.value)}
        onPaste={
          attach
            ? (event) => {
                const files = Array.from(event.clipboardData.files);
                if (files.length === 0) return;
                event.preventDefault();
                attach.add(files);
              }
            : undefined
        }
        onKeyDown={(event) => {
          // keyCode 229 is an IME composition in browsers that clear isComposing early (Safari).
          if (event.nativeEvent.isComposing || event.keyCode === 229) return;
          if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            onSend();
          } else if (event.key === 'Escape' && onStop) {
            event.preventDefault();
            if (!stopping) onStop();
          }
        }}
      />
      <div className="composer-foot">
        {attach ? (
          <span className="composer-attach">
            <input ref={fileRef} type="file" multiple hidden data-testid="attach-input" onChange={pick} />
            <button type="button" className="icon-btn" aria-label="Attach files" onClick={() => fileRef.current?.click()}>
              <span aria-hidden="true">📎</span>
            </button>
            {touch ? (
              <>
                <input ref={photoRef} type="file" accept="image/*" multiple hidden data-testid="photo-input" onChange={pick} />
                <button type="button" className="icon-btn" aria-label="Add a photo" onClick={() => photoRef.current?.click()}>
                  <span aria-hidden="true">📷</span>
                </button>
              </>
            ) : null}
          </span>
        ) : null}
        <span className="composer-hint muted small">{hint}</span>
        {stops ? (
          <button
            type="button"
            className="composer-send is-stop"
            aria-label={buttonLabel}
            aria-busy={stopping}
            disabled={stopping}
            onClick={() => onStop?.()}
          >
            <StopIcon />
            <span>{buttonLabel}</span>
          </button>
        ) : (
          <button type="submit" className="composer-send" aria-label="Send" disabled={sendDisabled}>
            <SendIcon />
            <span>Send</span>
          </button>
        )}
      </div>
    </form>
  );
}

/** What sits under the transcript: the composer, or why there is none. */
export function ComposerArea(props: {
  state: ReturnType<typeof composerState>;
  working: boolean;
  draft: string;
  onDraft: (v: string) => void;
  onSend: () => void;
  sending: boolean;
  /** Stops the running turn; the Stop button and Esc both call it. */
  onStop: () => void;
  stopping: boolean;
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  label: string;
  attach?: AttachmentUploads;
  /** The reader's position in the transcript, for the Jump to latest pill. */
  stick?: StickToBottom;
}) {
  const { state } = props;
  if (!state.enabled) return state.note ? <p className="muted small">{state.note}</p> : null;
  return (
    <div className="composer-dock">
      {props.stick && !props.stick.atBottom ? (
        <JumpToLatest unread={props.stick.unread} onClick={props.stick.jumpToLatest} />
      ) : null}
      <Composer
        textareaRef={props.textareaRef}
        draft={props.draft}
        onDraft={props.onDraft}
        onSend={props.onSend}
        disabled={false}
        sendDisabled={
          props.sending || !!props.attach?.uploading || (props.draft.trim() === '' && !props.attach?.ids.length)
        }
        attach={props.attach}
        placeholder={state.placeholder}
        label={props.label}
        onStop={props.working ? props.onStop : undefined}
        stopping={props.stopping}
        hint={
          props.working
            ? 'Enter to send · Shift+Enter for a new line · Esc stops the current turn'
            : 'Enter to send · Shift+Enter for a new line'
        }
      />
    </div>
  );
}
