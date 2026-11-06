// Small helpers for chat attachments.

export const DEFAULT_MAX_PER_MESSAGE = 10;

/** "812 B", "1.4 KB", "25 MB". */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB'];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 10 ? Math.round(v) : v.toFixed(1).replace(/\.0$/, '')} ${units[i]}`;
}

export const isImage = (mime: string): boolean => mime.startsWith('image/');

/** Formats every browser draws in an <img>. Other image types (SVG, which the server will not
 * serve inline, HEIC, BMP) stay a file tile that opens or downloads the file. */
const THUMBNAIL_TYPES = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);
export const canThumbnail = (mime: string): boolean => THUMBNAIL_TYPES.has(mime.split(';')[0].trim().toLowerCase());
