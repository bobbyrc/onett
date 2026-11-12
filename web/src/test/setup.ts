// Shared setup for component tests (files that opt in with `// @vitest-environment jsdom`).
// Pure-logic tests run in Node, so everything here must be safe without a DOM.
import { cleanup } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';

// React only warns about un-wrapped state updates when it is told it runs under a test.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Vitest globals are off, so Testing Library cannot register its own cleanup. Storage is wiped
// too, so a draft or a setting one test saves never reaches the next test in the file. Node tests
// have no window, and so no storage to wipe.
afterEach(() => {
  cleanup();
  if (typeof window !== 'undefined') {
    window.localStorage.clear();
    window.sessionStorage.clear();
  }
});
