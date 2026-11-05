import { useState } from 'react';
import { attachmentUrl } from '../api';
import { formatBytes, isImage } from '../lib/attachments';
import type { Attachment } from '../types';

/** A sent message's files: images as thumbnails, everything else as a chip that opens the file. */
export function MessageAttachments({ attachments }: { attachments: Attachment[] }) {
  return (
    <ul className="msg-atts" aria-label="Attached files">
      {attachments.map((a) => (
        <li key={a.id}>
          <MessageAttachment a={a} />
        </li>
      ))}
    </ul>
  );
}

function MessageAttachment({ a }: { a: Attachment }) {
  const [broken, setBroken] = useState(false);
  const href = attachmentUrl(a.conversation_id, a.id);
  // A file retention deleted answers 404; the broken image falls back to the plain chip.
  if (isImage(a.mime) && !broken) {
    return (
      <a href={href} target="_blank" rel="noopener noreferrer" className="msg-att-img" aria-label={`Open ${a.name}`}>
        <img src={href} alt={a.name} loading="lazy" onError={() => setBroken(true)} />
      </a>
    );
  }
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" className="att-chip msg-att-chip">
      <span className="att-icon" aria-hidden="true">
        📄
      </span>
      <span className="att-text">
        <span className="att-name" title={a.name}>
          {a.name}
        </span>
        <span className="att-size muted small">{formatBytes(a.size)}</span>
      </span>
    </a>
  );
}
