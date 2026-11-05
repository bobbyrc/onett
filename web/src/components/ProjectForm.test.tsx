// @vitest-environment jsdom
// Renders the Add project form for real with react-dom and jsdom (no Testing Library here).
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { ConfigApiError } from '../api';
import type { ProjectFormValues } from '../lib/projectForm';
import { ProjectForm } from './ProjectForm';

let root: Root | undefined;
let host: HTMLElement;

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});

afterEach(() => {
  act(() => root?.unmount());
  root = undefined;
  host?.remove();
});

function mount(props: Partial<React.ComponentProps<typeof ProjectForm>> = {}) {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  const onSubmit = vi.fn<(v: ProjectFormValues) => void>();
  act(() =>
    root!.render(
      <ProjectForm
        policies={['default', 'strict']}
        existing={['app']}
        pending={false}
        error={null}
        onSubmit={onSubmit}
        onCancel={() => {}}
        {...props}
      />,
    ),
  );
  return onSubmit;
}

function type(el: Element, value: string) {
  const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  act(() => {
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

const submit = () => act(() => host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));

describe('ProjectForm', () => {
  it('lists the configured policies in the dropdown', () => {
    mount();
    const options = [...host.querySelectorAll('select option')].map((o) => o.textContent);
    expect(options).toEqual(['Default', 'default', 'strict']);
  });

  it('refuses a taken name without calling onSubmit', () => {
    const onSubmit = mount();
    const [name, repo] = host.querySelectorAll('input');
    type(name!, 'app');
    type(repo!, 'acme/app');
    submit();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(host.textContent).toContain('already exists');
  });

  it('submits the values it was given', () => {
    const onSubmit = mount();
    const [name, repo] = host.querySelectorAll('input');
    type(name!, 'web');
    type(repo!, 'acme/web');
    type(host.querySelector('textarea')!, 'Be brief.');
    submit();
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ name: 'web', repo: 'acme/web', instructions: 'Be brief.' }));
  });

  it('shows the server problems inline', () => {
    mount({
      error: new ConfigApiError(422, 'the config is not valid', [{ path: 'projects.web.policy', message: 'unknown policy "x"' }]),
    });
    expect(host.textContent).toContain('projects.web.policy');
    expect(host.textContent).toContain('unknown policy');
  });
});
