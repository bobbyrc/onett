/**
 * The setup code that `onett open` puts after `#` in the address (`#setup=<code>`), which the
 * server needs before it creates the first account. Returns '' when the address has none.
 */
export function setupCodeFromHash(hash: string): string {
  const match = /^#setup=([0-9a-f]+)$/.exec(hash);
  return match ? match[1] : '';
}
