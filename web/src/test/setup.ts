// Shared setup for component tests (files that opt in with `// @vitest-environment jsdom`).
// Pure-logic tests run in Node, so everything here must be safe without a DOM.
import { cleanup } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';

// React only warns about un-wrapped state updates when it is told it runs under a test.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Vitest globals are off, so Testing Library cannot register its own cleanup.
afterEach(() => cleanup());
