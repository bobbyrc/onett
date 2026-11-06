import { describe, expect, it } from 'vitest';
import { deployStopped, deployWords, latestDeploys } from './boardDeploy';
import type { Deployment } from '../types';

const dep = (id: string, project: string, started_at: string, state: Deployment['state'] = 'healthy'): Deployment => ({
  id,
  project,
  ref: 'abc1234',
  provider_id: 'local',
  state,
  started_at,
  updated_at: started_at,
});

describe('latestDeploys', () => {
  it('keeps the newest deployment of each project, whatever the list order', () => {
    const latest = latestDeploys([
      dep('a1', 'a', '2026-11-01T10:00:00Z'),
      dep('b1', 'b', '2026-11-02T10:00:00Z'),
      dep('a2', 'a', '2026-11-03T10:00:00Z', 'rolled_back'),
      dep('a0', 'a', '2026-10-30T10:00:00Z'),
    ]);
    expect(latest.get('a')?.id).toBe('a2');
    expect(latest.get('b')?.id).toBe('b1');
    expect(latest.has('c')).toBe(false);
  });
});

describe('deployWords', () => {
  it('names every state in words', () => {
    expect(deployWords('healthy')).toBe('Deployed');
    expect(deployWords('rolled_back')).toBe('Rolled back');
    expect(deployWords('verifying')).toBe('Verifying deploy');
  });

  it('marks only a failed or rolled-back deploy as stopped', () => {
    expect(deployStopped('failed')).toBe(true);
    expect(deployStopped('rolled_back')).toBe(true);
    expect(deployStopped('deploying')).toBe(false);
    expect(deployStopped('healthy')).toBe(false);
  });
});
