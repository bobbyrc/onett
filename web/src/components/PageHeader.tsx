import { useEffect, useRef, type ReactNode } from 'react';
import { pageTitle } from '../lib/pageTitle';

/** A page's heading and its actions. The heading also names the browser tab. */
export function PageHeader({ title, actions }: { title: ReactNode; actions?: ReactNode }) {
  const heading = useRef<HTMLHeadingElement>(null);

  // The tab is named after the heading's own words, not a status pill that sits beside them.
  // After every render: see usePageTitle in lib/pageTitle.ts.
  useEffect(() => {
    const own = Array.from(heading.current?.childNodes ?? [], (node) => (node.nodeType === Node.TEXT_NODE ? node.textContent : ''))
      .join('')
      .trim();
    if (own) document.title = pageTitle(own);
  });

  return (
    <header className="page-header">
      <h1 ref={heading}>{title}</h1>
      {actions ? <div className="page-actions">{actions}</div> : null}
    </header>
  );
}
