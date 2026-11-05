// Answers every /api/v1 call from fixtures, and stands in for the /api/v1/ws socket, so a page
// renders with no daemon behind it.
import type { Page } from '@playwright/test';
import * as f from './fixtures';

type Json = unknown;

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
    case 'loops':
      return f.loops;
    case 'agents':
      return f.agents;
    case 'agent-conversations':
      return f.agentConversations;
    case 'conversations':
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

/** Installs the mocks. Returns the list of unmocked API paths the page asked for. */
export async function mockBackend(page: Page): Promise<string[]> {
  const unmocked: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/api\/v1/, '');
    const body = route.request().method() === 'GET' ? respond(path) : undefined;
    if (body === undefined) {
      unmocked.push(`${route.request().method()} ${path}`);
      await route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"not mocked"}' });
      return;
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
  // Accept the live-event socket and say nothing on it.
  await page.routeWebSocket(/\/api\/v1\/ws/, () => {});
  return unmocked;
}
