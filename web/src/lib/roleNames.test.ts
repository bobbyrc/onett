import { describe, expect, it } from 'vitest';
import { roleName } from './roleNames';

describe('roleName', () => {
  it('maps old built-in names to job roles', () => {
    expect(roleName('implementer')).toBe('senior-engineer');
    expect(roleName('orchestrator')).toBe('chief-of-staff');
    expect(roleName('reviewer')).toBe('code-reviewer');
    expect(roleName('triager')).toBe('tech-lead');
    expect(roleName('improver')).toBe('platform-engineer');
  });
  it('leaves current, custom and empty names alone', () => {
    expect(roleName('senior-engineer')).toBe('senior-engineer');
    expect(roleName('my-agent')).toBe('my-agent');
    expect(roleName('')).toBe('');
    expect(roleName('constructor')).toBe('constructor');
  });
});
