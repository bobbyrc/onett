// The browser tab's title: "<page> · onett". A route names the tab at once (useRouteTitle, called
// by Shell); a page that knows a better name, such as a task's title, then sets it (PageHeader
// does, from its heading). Without this every tab, history entry and bookmark reads "onett".
import { useEffect, useLayoutEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { shortId } from './format';

const APP = 'onett';

/** The tab title for a page called `name`: "Tasks · onett". With no name it is "onett". */
export function pageTitle(name?: string): string {
  return name ? `${name} · ${APP}` : APP;
}

// First path segment → the name of the list page, and of one item where an item has a page.
const SECTIONS: Record<string, [list: string, item?: string]> = {
  '': ['Overview'],
  projects: ['Projects'],
  chat: ['Chat'],
  goals: ['Goals', 'Goal'],
  tasks: ['Tasks', 'Task'],
  runs: ['Runs', 'Run'],
  approvals: ['Approvals'],
  deploys: ['Deploys'],
  signals: ['Signals'],
  friction: ['Friction'],
  loops: ['Loops'],
  triggers: ['Triggers', 'Trigger'],
  agents: ['Agents'],
  config: ['Config'],
  users: ['Users'],
};

/**
 * The name a route has before its page has drawn anything: "Tasks" for /tasks, and
 * "Task 9W4Y6ABC" for /tasks/<id>. Undefined for a route this table does not know.
 */
export function routeTitle(pathname: string): string | undefined {
  const [section = '', id] = pathname.split('/').filter(Boolean);
  const names = SECTIONS[section];
  if (!names) return undefined;
  const [list, item] = names;
  return id && item ? `${item} ${shortId(id)}` : list;
}

/**
 * Names the tab after the current route. Shell calls it once. It is a layout effect on purpose:
 * layout effects run before ordinary effects, so a page that sets its own name in the same
 * render (usePageTitle, PageHeader) always has the last word.
 */
export function useRouteTitle(): void {
  const { pathname } = useLocation();
  useLayoutEffect(() => {
    document.title = pageTitle(routeTitle(pathname));
  }, [pathname]);
}

/**
 * Names the tab after the page that calls it. It runs after every render, not only when the
 * name changes, because the route may have renamed the tab in between (two tasks can share a
 * title). An empty name leaves the route's name in place.
 */
export function usePageTitle(name: string | undefined): void {
  useEffect(() => {
    if (name) document.title = pageTitle(name);
  });
}
