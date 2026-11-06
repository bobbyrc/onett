import { describe, expect, it } from 'vitest';
import { taskSourceLabel } from './taskSource';

const CHAT = '01a1af1c-27cc-7007-a8b8-d595656c8f28';

describe('taskSourceLabel', () => {
  it('names a chat source and links to its conversation', () => {
    expect(taskSourceLabel({ source: `chat:${CHAT}` })).toEqual({ text: 'chat', href: `/chat/${CHAT}`, ref: '' });
  });
  it('keeps plain sources as they are', () => {
    expect(taskSourceLabel({ source: 'api' })).toEqual({ text: 'api', ref: '' });
    expect(taskSourceLabel({ source: 'github_issue', source_ref: 'github_issue:42' })).toEqual({
      text: 'github_issue',
      ref: 'github_issue:42',
    });
  });
  it('links a follow-up to its parent task', () => {
    expect(taskSourceLabel({ source: 'followup', parent_id: 'p1', source_ref: `chat:${CHAT}` })).toEqual({
      text: 'follow-up of parent task',
      href: '/tasks/p1',
      ref: '',
    });
  });
  it('links a follow-up without a parent to the chat in its ref', () => {
    expect(taskSourceLabel({ source: 'followup', source_ref: `chat:${CHAT}` })).toEqual({
      text: 'follow-up from chat',
      href: `/chat/${CHAT}`,
      ref: '',
    });
  });
  it('never puts a conversation id in the text', () => {
    const l = taskSourceLabel({ source: `chat:${CHAT}`, source_ref: `chat:${CHAT}` });
    expect(JSON.stringify(l).replace(l.href ?? '', '')).not.toContain(CHAT);
  });
});
