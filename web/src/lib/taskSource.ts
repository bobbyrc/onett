import type { Task } from '../types';

const CHAT_PREFIX = 'chat:';

export interface TaskSourceLabel {
  /** Human wording for where the task came from, never a raw id. */
  text: string;
  /** Where the label links to (the chat or the parent task), when there is one. */
  href?: string;
  /** The source_ref worth showing next to the label; empty when it is an id or repeats the label. */
  ref: string;
}

/** The chat conversation id a task is linked to: in source, or in source_ref for a follow-up. */
function chatId(task: Pick<Task, 'source' | 'source_ref'>): string {
  for (const s of [task.source, task.source_ref ?? '']) {
    if (s.startsWith(CHAT_PREFIX) && s.length > CHAT_PREFIX.length) return s.slice(CHAT_PREFIX.length);
  }
  return '';
}

/** Turns a task's source fields into wording and a link, so a conversation uuid never reaches the screen. */
export function taskSourceLabel(task: Pick<Task, 'source' | 'source_ref' | 'parent_id'>): TaskSourceLabel {
  const chat = chatId(task);
  const ref = task.source_ref && !task.source_ref.startsWith(CHAT_PREFIX) ? task.source_ref : '';
  if (task.source.startsWith(CHAT_PREFIX)) return { text: 'chat', href: `/chat/${chat}`, ref };
  if (task.source === 'followup' || task.source === 'conversation') {
    if (task.parent_id) return { text: 'follow-up of parent task', href: `/tasks/${task.parent_id}`, ref };
    if (chat) return { text: 'follow-up from chat', href: `/chat/${chat}`, ref };
    return { text: 'follow-up', ref };
  }
  return { text: task.source, ref };
}
