// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeAll, expect, it, vi } from 'vitest';
import { NodeEditor } from './ConfigSection';
import * as api from '../../api';
import type { AgentView, SchemaNode } from '../../types';

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});
afterEach(() => vi.restoreAllMocks());

const node: SchemaNode = { key: 'agents', type: 'map', item: { key: 'agent', type: 'section', fields: [] } };

it('names built-in agents the file does not write', async () => {
  const view = (name: string, origin: AgentView['origin']) => ({ name, origin }) as AgentView;
  vi.spyOn(api, 'listAgents').mockResolvedValue([
    view('implementer', 'builtin'),
    view('reviewer', 'edited'),
  ]);
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <NodeEditor
          node={node}
          path={['agents']}
          ctx={{ draft: {}, problems: new Map(), setValue: () => {}, clearValue: () => {} }}
          nested={false}
        />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
  const note = host.querySelector('[data-testid="builtin-agents-note"]');
  expect(note?.textContent).toContain('implementer');
  expect(note?.textContent).not.toContain('reviewer');
  act(() => root.unmount());
});
