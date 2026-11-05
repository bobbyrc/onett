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
