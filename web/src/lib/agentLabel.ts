import { roleName } from './roleNames';

// Words that stay upper case when a key is turned into a label ("sre" → "SRE").
const ACRONYMS = new Set(['sre', 'ci', 'qa', 'ui', 'ux', 'api']);
// Small words that stay lower case inside a label, like the built-in title "Chief of Staff".
const SMALL_WORDS = new Set(['of', 'and', 'the', 'for', 'to', 'in', 'on']);

/** Turns an agent key into Title Case words, the way the built-in titles are written. */
export function humanizeAgentName(key: string): string {
  return key
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((word, i) => {
      const lower = word.toLowerCase();
      if (ACRONYMS.has(lower)) return lower.toUpperCase();
      if (i > 0 && SMALL_WORDS.has(lower)) return lower;
      return lower.charAt(0).toUpperCase() + lower.slice(1);
    })
    .join(' ');
}

/**
 * The text to show for an agent: its title when set, else its key made readable. Only a display
 * label: URLs, form values, config and CLI keep the raw key. An old built-in name (a stored
 * "implementer") is mapped to its current role first. A plain string is looked up in `agents`
 * (when given) for a title.
 */
export function agentLabel(agent: string | { name: string; title?: string }, agents?: { name: string; title?: string }[]): string {
  const key = typeof agent === 'string' ? roleName(agent) : agent.name;
  const title = typeof agent === 'string' ? agents?.find((a) => a.name === key)?.title : agent.title;
  return title || humanizeAgentName(key);
}
