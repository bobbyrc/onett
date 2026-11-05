import { describe, expect, it } from 'vitest';
import { emptyProjectForm, projectFormError, projectFormRequest } from './projectForm';

const ok = { ...emptyProjectForm, name: 'web', repo: 'acme/web' };

describe('projectFormError', () => {
  it('accepts a name and an owner/name repo', () => {
    expect(projectFormError(ok, [])).toBeNull();
  });
  it('refuses a missing, malformed or taken name', () => {
    expect(projectFormError({ ...ok, name: ' ' }, [])).toMatch(/name is required/);
    expect(projectFormError({ ...ok, name: 'we b' }, [])).toMatch(/letters, digits/);
    expect(projectFormError(ok, ['web'])).toMatch(/already exists/);
  });
  it('refuses a repository that is not owner/name', () => {
    expect(projectFormError({ ...ok, repo: '' }, [])).toMatch(/repository is required/);
    expect(projectFormError({ ...ok, repo: 'justname' }, [])).toMatch(/owner\/name/);
    expect(projectFormError({ ...ok, repo: '/srv/repo' }, [])).toBeNull();
  });
});

describe('projectFormRequest', () => {
  it('leaves empty optional fields out', () => {
    expect(projectFormRequest({ ...ok, name: ' web ', branch: ' ' })).toEqual({ name: 'web', repo: 'acme/web' });
  });
  it('sends the optional fields that are set', () => {
    expect(
      projectFormRequest({ ...ok, path: '/srv/web', branch: 'dev', policy: 'strict', instructions: 'Be brief.\n' }),
    ).toEqual({ name: 'web', repo: 'acme/web', path: '/srv/web', branch: 'dev', policy: 'strict', instructions: 'Be brief.\n' });
  });
});
