// Wording and checks for the Secrets panel. Pure, so they are tested without a browser.
import type { SecretView } from '../types';

/** "set" or "not set". */
export function secretStatus(s: SecretView): string {
  return s.set ? 'set' : 'not set';
}

/** Where a set value comes from, in words. Empty for an unset secret. */
export function sourceLabel(s: SecretView): string {
  if (s.source === 'file') return 'secrets.env';
  if (s.source === 'environment') return "daemon's environment";
  return '';
}

/**
 * Whether a saved value is overridden. The daemon's real environment wins over secrets.env, so
 * a value saved here is stored but not in effect until the environment variable is removed.
 */
export function isShadowed(s: SecretView): boolean {
  return s.source === 'environment';
}

/** Why a value cannot be saved, or null. The server checks again. */
export function valueProblem(value: string): string | null {
  if (value === '') return 'Enter a value.';
  if (/[\r\n]/.test(value)) return 'A secret must be a single line.';
  return null;
}

/** Referenced names that are not set come first, since they are what the config needs. */
export function sortSecrets(list: SecretView[]): SecretView[] {
  const rank = (s: SecretView) => (!s.set && s.referenced_by.length > 0 ? 0 : s.set ? 1 : 2);
  return [...list].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

/** A secret name: capital letters, digits and underscores, as the server requires. */
export const SECRET_NAME_RE = /^[A-Z_][A-Z0-9_]*$/;

/**
 * The names a `*_env` field can pick: every known secret, plus the field's current value when it
 * is not one of them (a typo, or a variable only the daemon's environment has), so the dropdown
 * never shows something other than what the file holds.
 */
export function pickerNames(list: SecretView[], current: string): string[] {
  const names = list.map((s) => s.name).sort((a, b) => a.localeCompare(b));
  return current !== '' && !names.includes(current) ? [...names, current] : names;
}
