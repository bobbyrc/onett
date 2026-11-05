import type { Stats } from '../types';

export const OWNER_UNREACHABLE_MESSAGE =
  'Only the log notifier is configured, so onett cannot tell you when it needs you. Set up a channel or notifier (Telegram, Discord or ntfy): see docs/channels.md.';

/** The warning to show for these stats: null when the owner can be reached, else the text and a line per blocked goal. */
export function ownerWarning(stats: Pick<Stats, 'owner_unreachable' | 'blocked_goals'> | undefined): { message: string; goals: { id: string; title: string; text: string }[] } | null {
  if (!stats?.owner_unreachable) return null;
  return {
    message: OWNER_UNREACHABLE_MESSAGE,
    goals: (stats.blocked_goals ?? []).map((g) => ({
      id: g.id,
      text: `is blocked and you have not been notified: ${g.reason || 'no reason was recorded'}`,
      title: g.title || g.id,
    })),
  };
}
