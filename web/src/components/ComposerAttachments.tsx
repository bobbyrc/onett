import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, uploadAttachment, type Upload } from '../api';
import { formatBytes, isImage } from '../lib/attachments';
import { cx, errorMessage } from '../lib/format';

/** A file the person picked, on its way to the server. */
export interface PendingFile {
  key: string;
  file: File;
  /** Object URL of an image, for the thumbnail. Revoked when the file is removed. */
  preview?: string;
  status: 'uploading' | 'done' | 'error';
  progress: number;
  /** The attachment id once uploaded. */
  id?: string;
  error?: string;
}

export interface AttachmentUploads {
  items: PendingFile[];
  /** Starts uploading each file; any beyond the per-message limit shows as an error chip. */
  add: (files: File[]) => void;
  remove: (key: string) => void;
  /** Forgets everything after a send (the files now belong to the message). */
  clear: () => void;
  uploading: boolean;
  /** Ids of the files that uploaded, in the order they were picked. */
  ids: string[];
}

const explain = (err: unknown): string => {
  if (err instanceof ApiError && err.status === 413) return `Too large: ${errorMessage(err)}`;
  return errorMessage(err);
};

/** Uploads on selection and tracks each file. Object URLs are revoked on removal and on unmount. */
export function useAttachmentUploads(conversationId: string, max: number): AttachmentUploads {
  const [items, setItems] = useState<PendingFile[]>([]);
  const itemsRef = useRef<PendingFile[]>([]);
  const uploads = useRef(new Map<string, Upload>());
  const seq = useRef(0);
  itemsRef.current = items;

  const patch = useCallback((key: string, change: Partial<PendingFile>) => {
    setItems((all) => all.map((it) => (it.key === key ? { ...it, ...change } : it)));
  }, []);

  const add = useCallback(
    (files: File[]) => {
      let slots = max - itemsRef.current.filter((it) => it.status !== 'error').length;
      const fresh: PendingFile[] = files.map((file) => {
        const key = `f${++seq.current}`;
        const preview = isImage(file.type) ? URL.createObjectURL(file) : undefined;
        if (slots <= 0) {
          return { key, file, preview, status: 'error', progress: 0, error: `Only ${max} files fit in one message` };
        }
        slots--;
        return { key, file, preview, status: 'uploading', progress: 0 };
      });
      if (fresh.length === 0) return;
      setItems((all) => [...all, ...fresh]);
      for (const it of fresh) {
        if (it.status !== 'uploading') continue;
        const up = uploadAttachment(conversationId, it.file, (p) => patch(it.key, { progress: p }));
        uploads.current.set(it.key, up);
        up.done.then(
          (a) => patch(it.key, { status: 'done', progress: 1, id: a.id }),
          (err) => {
            // A removed file aborts its upload; nobody is left to read that error.
            if (uploads.current.has(it.key)) patch(it.key, { status: 'error', error: explain(err) });
          },
        );
      }
    },
    [conversationId, max, patch],
  );

  const remove = useCallback((key: string) => {
    uploads.current.get(key)?.abort();
    uploads.current.delete(key);
    const gone = itemsRef.current.find((it) => it.key === key);
    if (gone?.preview) URL.revokeObjectURL(gone.preview);
    setItems((all) => all.filter((it) => it.key !== key));
  }, []);

  const clear = useCallback(() => {
    uploads.current.clear();
    for (const it of itemsRef.current) if (it.preview) URL.revokeObjectURL(it.preview);
    setItems([]);
  }, []);

  useEffect(
    () => () => {
      for (const up of uploads.current.values()) up.abort();
      uploads.current.clear();
      for (const it of itemsRef.current) if (it.preview) URL.revokeObjectURL(it.preview);
    },
    [],
  );

  return {
    items,
    add,
    remove,
    clear,
    uploading: items.some((it) => it.status === 'uploading'),
    ids: items.flatMap((it) => (it.status === 'done' && it.id ? [it.id] : [])),
  };
}

/**
 * Files chosen before the conversation exists (the start screen). Nothing is uploaded yet: each file
 * shows as ready, and `files()` hands them to the conversation, which uploads them with the first message.
 */
export function useLocalAttachments(max: number): AttachmentUploads & { files: () => File[] } {
  const [items, setItems] = useState<PendingFile[]>([]);
  const itemsRef = useRef<PendingFile[]>([]);
  const seq = useRef(0);
  itemsRef.current = items;

  const revoke = (it: PendingFile) => {
    if (it.preview) URL.revokeObjectURL(it.preview);
  };
  const add = useCallback(
    (files: File[]) => {
      let slots = max - itemsRef.current.filter((it) => it.status !== 'error').length;
      const fresh = files.map((file): PendingFile => {
        const key = `l${++seq.current}`;
        const preview = isImage(file.type) ? URL.createObjectURL(file) : undefined;
        if (slots <= 0) {
          return { key, file, preview, status: 'error', progress: 0, error: `Only ${max} files fit in one message` };
        }
        slots--;
        return { key, file, preview, status: 'done', progress: 1, id: key };
      });
      if (fresh.length > 0) setItems((all) => [...all, ...fresh]);
    },
    [max],
  );
  const remove = useCallback((key: string) => {
    const gone = itemsRef.current.find((it) => it.key === key);
    if (gone) revoke(gone);
    setItems((all) => all.filter((it) => it.key !== key));
  }, []);
  const clear = useCallback(() => {
    itemsRef.current.forEach(revoke);
    setItems([]);
  }, []);
  useEffect(() => () => itemsRef.current.forEach(revoke), []);

  return {
    items,
    add,
    remove,
    clear,
    uploading: false,
    ids: items.flatMap((it) => (it.status === 'done' && it.id ? [it.id] : [])),
    files: () => items.filter((it) => it.status === 'done').map((it) => it.file),
  };
}

/** The chips above the field: a thumbnail or icon, the name, progress or an error, and Remove. */
export function PendingChips({ uploads }: { uploads: AttachmentUploads }) {
  if (uploads.items.length === 0) return null;
  return (
    <ul className="att-chips" aria-label="Attachments">
      {uploads.items.map((it) => (
        <li key={it.key} className={cx('att-chip', it.status === 'error' && 'is-error')}>
          {it.preview ? (
            <img className="att-thumb" src={it.preview} alt="" />
          ) : (
            <span className="att-icon" aria-hidden="true">
              📄
            </span>
          )}
          <span className="att-text">
            <span className="att-name" title={it.file.name}>
              {it.file.name}
            </span>
            {it.status === 'uploading' ? (
              <progress className="att-progress" max={100} value={Math.round(it.progress * 100)} aria-label={`Uploading ${it.file.name}`} />
            ) : it.status === 'error' ? (
              <span className="att-error small" role="alert">
                {it.error}
              </span>
            ) : (
              <span className="att-size muted small">{formatBytes(it.file.size)}</span>
            )}
          </span>
          <button type="button" className="att-remove" aria-label={`Remove ${it.file.name}`} onClick={() => uploads.remove(it.key)}>
            <span aria-hidden="true">×</span>
          </button>
        </li>
      ))}
    </ul>
  );
}
