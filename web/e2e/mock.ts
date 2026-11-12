// Answers every /api/v1 call from fixtures, and stands in for the /api/v1/ws socket (it confirms
// subscribes and sends nothing else), so a page renders with no daemon behind it.
import type { Page } from '@playwright/test';
import * as f from './fixtures';
import type { ConversationSettingsView } from '../src/types';

type Json = unknown;

const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');

/** The mocked body for a GET path (without /api/v1), or undefined when no fixture covers it. */
export function respond(path: string): Json | undefined {
  const parts = path.split('/').filter(Boolean);
  const [head, a, b] = parts;
  switch (head) {
    case 'auth':
      return a === 'state' ? f.authState : undefined;
    case 'health':
      return { ok: true, version: 'e2e' };
    case 'stats':
      return f.stats;
    case 'control':
      return f.control;
    case 'users':
      return f.users;
    case 'secrets':
      return { secrets: f.secrets };
    case 'projects':
      return f.projects;
    case 'tasks':
      if (!a) return f.tasks;
      if (b === 'conversation') return f.taskConversation;
      return {
        task: f.tasks.find((t) => t.id === a) ?? f.tasks[0],
        runs: f.runs,
        approvals: f.approvals,
        children: f.tasks.slice(1, 4),
      };
    case 'goals':
      return a ? { goal: f.goals[0], tasks: f.tasks } : f.goals;
    case 'triggers':
      if (!a) return f.triggers;
      if (b === 'firings') return f.triggerFirings.filter((x) => x.trigger_id === a);
      return f.triggers.find((t) => t.id === a) ?? f.triggers[0];
    case 'trigger-firings':
      return f.triggerFirings;
    case 'runs':
      if (!a) return f.runs;
      return b === 'logs' ? f.logs : (f.runs.find((r) => r.id === a) ?? f.runs[0]);
    case 'approvals':
      return f.approvals;
    case 'deployments':
      return f.deployments;
    case 'signals':
      return f.signals;
    case 'friction':
      return f.friction;
    case 'problems':
      return f.problems;
    case 'loops':
      return f.loops;
    case 'agents':
      return f.agents;
    case 'agent-conversations':
      return f.agentConversations;
    case 'conversations':
      if (a && b === 'settings') return SETTINGS_VIEW;
      return a ? { conversation: f.conversations[0], messages: f.chatMessages } : f.conversations;
    case 'events':
      return [];
    case 'config':
      if (a === 'document') return f.configDocument;
      if (a === 'file') return f.configText;
      return f.config;
    default:
      return undefined;
  }
}

const SETTINGS_VIEW: ConversationSettingsView = {
  settings: { model: 'opus', effort: 'high', permission_mode: 'acceptEdits' },
  defaults: { model: 'sonnet' },
  harness: 'claude',
  choices: {
    models: [
      { value: 'opus', label: 'Opus' },
      { value: 'sonnet', label: 'Sonnet' },
      { value: 'haiku', label: 'Haiku' },
    ],
    efforts: ['low', 'medium', 'high', 'xhigh', 'max'].map((v) => ({ value: v, label: v[0].toUpperCase() + v.slice(1) })),
    permission_modes: [
      { value: 'auto', label: 'Auto' },
      { value: 'acceptEdits', label: 'Accept edits' },
      { value: 'plan', label: 'Plan' },
      { value: 'bypassPermissions', label: 'Bypass permissions', dangerous: true },
    ],
  },
};

/** Installs the mocks. Returns the list of unmocked API paths the page asked for. */
export async function mockBackend(page: Page): Promise<string[]> {
  const unmocked: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/api\/v1/, '');
    // Chat attachments: uploads succeed, and any download is a 1x1 PNG.
    if (/^\/conversations\/[^/]+\/attachments(\/|$)/.test(path)) {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ id: `att-${Date.now()}-${Math.random()}`, name: 'x', mime: 'application/octet-stream', size: 1 }),
        });
      } else {
        await route.fulfill({ status: 200, contentType: 'image/png', body: PNG });
      }
      return;
    }
    const body = route.request().method() === 'GET' ? respond(path) : undefined;
    if (body === undefined) {
      unmocked.push(`${route.request().method()} ${path}`);
      await route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"not mocked"}' });
      return;
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
  // Accept the live-event socket. It sends no events, but it must confirm every subscribe: the
  // UI keeps its live light on "reconnecting…" until a chat or run watch is confirmed.
  await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
    ws.onMessage((raw) => {
      const msg = JSON.parse(String(raw)) as { kind: string; run_id?: string; conversation_id?: string };
      if (msg.kind === 'subscribe') ws.send(JSON.stringify({ kind: 'subscribed', run_id: msg.run_id, conversation_id: msg.conversation_id }));
    });
  });
  return unmocked;
}
