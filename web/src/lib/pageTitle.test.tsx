// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { PageHeader } from '../components/PageHeader';
import { pageTitle, routeTitle, usePageTitle, useRouteTitle } from './pageTitle';

beforeEach(() => {
  document.title = 'onett';
});
afterEach(cleanup);

describe('pageTitle', () => {
  it('puts the page name before the app name', () => {
    expect(pageTitle('Tasks')).toBe('Tasks · onett');
  });

  it('is the app name alone when the page has no name', () => {
    expect(pageTitle()).toBe('onett');
    expect(pageTitle('')).toBe('onett');
  });
});

describe('routeTitle', () => {
  it('names each list page', () => {
    expect(routeTitle('/')).toBe('Overview');
    expect(routeTitle('/tasks')).toBe('Tasks');
    expect(routeTitle('/runs')).toBe('Runs');
    expect(routeTitle('/goals')).toBe('Goals');
    expect(routeTitle('/chat')).toBe('Chat');
    expect(routeTitle('/config')).toBe('Config');
    expect(routeTitle('/users')).toBe('Users');
  });

  it('names a task, run or goal by the short form of its ID', () => {
    expect(routeTitle('/tasks/01HZX01K8M3Q7R2T5V9W4Y6ABC')).toBe('Task 9W4Y6ABC');
    expect(routeTitle('/runs/01HZX03K8M3Q7R2T5V9W4Y6ABC')).toBe('Run 9W4Y6ABC');
    expect(routeTitle('/goals/01HZX02K8M3Q7R2T5V9W4Y6ABC')).toBe('Goal 9W4Y6ABC');
  });

  it('keeps the section name where one item has no name of its own', () => {
    expect(routeTitle('/chat/01HZX04K8M3Q7R2T5V9W4Y6ABC')).toBe('Chat');
    expect(routeTitle('/chat/task/01HZX01K8M3Q7R2T5V9W4Y6ABC')).toBe('Chat');
  });

  it('has no name for a route it does not know', () => {
    expect(routeTitle('/nowhere')).toBeUndefined();
  });
});

function Named({ name }: { name: string | undefined }) {
  usePageTitle(name);
  return null;
}

/** A detail page as Shell sees it: the route names it first, the heading once it has loaded. */
function DetailPage({ loaded }: { loaded: boolean }) {
  useRouteTitle();
  return loaded ? <PageHeader title="Fix the flaky test" /> : <p>Loading task…</p>;
}

describe('a page sets its title', () => {
  it('usePageTitle names the tab', () => {
    render(<Named name="Sign in" />);
    expect(document.title).toBe('Sign in · onett');
  });

  it('usePageTitle leaves the title alone without a name', () => {
    document.title = 'Tasks · onett';
    render(<Named name={undefined} />);
    expect(document.title).toBe('Tasks · onett');
  });

  it('PageHeader names the tab after its heading', () => {
    render(<PageHeader title="Tasks" />);
    expect(document.title).toBe('Tasks · onett');
  });

  it('PageHeader leaves out a status pill beside the title', () => {
    render(
      <PageHeader
        title={
          <>
            Fix the flaky test <span className="pill">running</span>
          </>
        }
      />,
    );
    expect(document.title).toBe('Fix the flaky test · onett');
  });

  it('the route names a detail page until its heading is drawn, then the heading does', () => {
    const at = (loaded: boolean) => (
      <MemoryRouter initialEntries={['/tasks/01HZX01K8M3Q7R2T5V9W4Y6ABC']}>
        <DetailPage loaded={loaded} />
      </MemoryRouter>
    );
    const view = render(at(false));
    expect(document.title).toBe('Task 9W4Y6ABC · onett');
    view.rerender(at(true));
    expect(document.title).toBe('Fix the flaky test · onett');
  });

  it('the heading wins when the route and the heading are drawn together', () => {
    render(
      <MemoryRouter initialEntries={['/tasks/01HZX01K8M3Q7R2T5V9W4Y6ABC']}>
        <DetailPage loaded />
      </MemoryRouter>,
    );
    expect(document.title).toBe('Fix the flaky test · onett');
  });
});
