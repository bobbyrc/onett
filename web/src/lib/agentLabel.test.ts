import { describe, expect, it } from 'vitest';
import { agentLabel, humanizeAgentName } from './agentLabel';

describe('humanizeAgentName', () => {
  it('splits on dashes and underscores and capitalises each word', () => {
    expect(humanizeAgentName('implementer-claude')).toBe('Implementer Claude');
    expect(humanizeAgentName('my_custom_agent')).toBe('My Custom Agent');
  });
  it('keeps acronyms upper case and small words lower case', () => {
    expect(humanizeAgentName('sre')).toBe('SRE');
    expect(humanizeAgentName('qa-ui-api')).toBe('QA UI API');
    expect(humanizeAgentName('chief-of-staff')).toBe('Chief of Staff');
  });
  it('handles empty input', () => {
    expect(humanizeAgentName('')).toBe('');
  });
});

describe('agentLabel', () => {
  it('prefers the title', () => {
    expect(agentLabel({ name: 'sre', title: 'Site Reliability Engineer' })).toBe('Site Reliability Engineer');
    expect(agentLabel('sre', [{ name: 'sre', title: 'Site Reliability Engineer' }])).toBe('Site Reliability Engineer');
  });
  it('falls back to the humanized key', () => {
    expect(agentLabel({ name: 'implementer-claude' })).toBe('Implementer Claude');
    expect(agentLabel('implementer-claude')).toBe('Implementer Claude');
  });
  it('maps old built-in names first', () => {
    expect(agentLabel('implementer')).toBe('Senior Engineer');
    expect(agentLabel('')).toBe('');
  });
});
