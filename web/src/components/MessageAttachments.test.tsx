// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { canThumbnail } from '../lib/attachments';
import type { Attachment } from '../types';
import { MessageAttachments } from './MessageAttachments';

afterEach(cleanup);

const att = (id: string, name: string, mime: string): Attachment => ({
  id,
  conversation_id: 'c1',
  name,
  mime,
  size: 2048,
  created_at: '',
});

describe('canThumbnail', () => {
  it('accepts the four raster types the server serves inline', () => {
    for (const m of ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'IMAGE/PNG', 'image/png; charset=binary']) {
      expect(canThumbnail(m), m).toBe(true);
    }
  });

  it('leaves SVG, HEIC and non-images as file tiles', () => {
    for (const m of ['image/svg+xml', 'image/heic', 'image/bmp', 'application/pdf', 'application/octet-stream']) {
      expect(canThumbnail(m), m).toBe(false);
    }
  });
});

describe('MessageAttachments', () => {
  it('shows each image type as a thumbnail that opens the full image in a new tab', () => {
    const files = [
      att('a1', 'one.png', 'image/png'),
      att('a2', 'two.jpg', 'image/jpeg'),
      att('a3', 'three.gif', 'image/gif'),
      att('a4', 'four.webp', 'image/webp'),
    ];
    render(<MessageAttachments attachments={files} />);
    for (const f of files) {
      const img = screen.getByAltText(f.name);
      expect(img).toHaveAttribute('src', `/api/v1/conversations/c1/attachments/${f.id}`);
      const link = img.closest('a');
      expect(link).toHaveAttribute('href', `/api/v1/conversations/c1/attachments/${f.id}`);
      expect(link).toHaveAttribute('target', '_blank');
      expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
    }
  });

  it('keeps the file tile for a PDF and for an image type a browser cannot draw', () => {
    render(<MessageAttachments attachments={[att('a5', 'notes.pdf', 'application/pdf'), att('a6', 'vector.svg', 'image/svg+xml')]} />);
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.getByRole('link', { name: /notes\.pdf/ })).toHaveTextContent('2 KB');
    expect(screen.getByRole('link', { name: /vector\.svg/ })).toBeInTheDocument();
  });
});
