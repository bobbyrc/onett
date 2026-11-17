import { describe, expect, it } from 'vitest';
import type { InspectResult } from '../types';
import {
  addBlocker,
  addProjectRequest,
  firstTaskTitle,
  guessedFrom,
  initialValues,
  PROJECT_NAME,
  type AddProjectValues,
} from './projectForm';

const result = (over: Partial<InspectResult> = {}): InspectResult => ({
  name: 'pimlo',
  repo: 'bobbyrc/pimlo',
  github: true,
  private: true,
  copy: { ok: true, error: '' },
  default_branch: 'main',
  branch_from: 'github',
  pull_requests: { ok: true, gh_user: 'bobbyrc', error: '' },
  guess: {
    setup: [{ name: 'npm ci', run: 'npm ci', from: 'package-lock.json' }],
    checks: [
      { name: 'npm run lint', run: 'npm run lint', from: 'package.json' },
      { name: 'npm test', run: 'npm test', from: 'package.json' },
    ],
    top_files: ['package.json'],
  },
  suggest: '',
  ...over,
});

const values = (over: Partial<AddProjectValues> = {}): AddProjectValues => ({
  ...initialValues(result()),
  ...over,
});

describe('initialValues', () => {
  it('starts from the check: name, branch, the guessed commands and supervised', () => {
    expect(initialValues(result())).toEqual({
      name: 'pimlo',
      branch: 'main',
      policy: 'supervised',
      instructions: '',
      setup: ['npm ci'],
      checks: ['npm run lint', 'npm test'],
      none: false,
    });
  });
});

describe('guessedFrom', () => {
  it('lists the distinct source files, setup first, in order', () => {
    expect(guessedFrom(result())).toEqual(['package-lock.json', 'package.json']);
  });
  it('is empty when nothing was guessed', () => {
    const r = result({ guess: { setup: [], checks: [], top_files: [] } });
    expect(guessedFrom(r)).toEqual([]);
  });
});

describe('addBlocker', () => {
  it('is null when the values can be added', () => {
    expect(addBlocker(result(), values(), [])).toBeNull();
  });

  it('refuses a repository that could not be copied', () => {
    const r = result({ copy: { ok: false, error: 'not found' } });
    expect(addBlocker(r, values(), [])).toBe('onett adds a project only once it can copy it.');
  });

  it('refuses a missing default branch unless the branch is typed in', () => {
    const r = result({ default_branch: '' });
    expect(addBlocker(r, values({ branch: '' }), [])).toBe('onett could not read the default branch; set one under Advanced.');
    expect(addBlocker(r, values({ branch: 'dev' }), [])).toBeNull();
  });

  it('refuses a GitHub repository whose pull requests cannot be opened', () => {
    const r = result({ pull_requests: { ok: false, gh_user: '', error: 'signed out' } });
    expect(addBlocker(r, values(), [])).toBe('onett cannot open pull requests until the GitHub CLI is signed in.');
  });

  it('allows a non-GitHub repository without pull requests', () => {
    const r = result({ github: false, pull_requests: { ok: false, gh_user: '', error: '' } });
    expect(addBlocker(r, values(), [])).toBeNull();
  });

  it('refuses a name that is blank or not allowed, and a taken one', () => {
    const message = 'The name may use letters, digits, ".", "_" and "-".';
    expect(addBlocker(result(), values({ name: '' }), [])).toBe(message);
    expect(addBlocker(result(), values({ name: 'we b' }), [])).toBe(message);
    expect(addBlocker(result(), values({ name: 'app' }), ['app'])).toBe('A project named app already exists.');
  });

  it('refuses an empty guess left empty, unless no setup or checks is chosen', () => {
    const empty = { setup: [''], checks: ['  '], none: false };
    expect(addBlocker(result(), values(empty), [])).toBe('Fill in a command, or choose no setup or checks.');
    expect(addBlocker(result(), values({ ...empty, none: true }), [])).toBeNull();
    expect(addBlocker(result(), values({ setup: [], checks: [], none: false }), [])).toBe(
      'Fill in a command, or choose no setup or checks.',
    );
  });

  it('reports the first problem that applies', () => {
    const r = result({ copy: { ok: false, error: 'x' } });
    expect(addBlocker(r, values({ name: '' }), [])).toBe('onett adds a project only once it can copy it.');
  });
});

describe('addProjectRequest', () => {
  it('omits the name when it is the one onett chose, and sends the others', () => {
    expect(addProjectRequest(result(), values())).toEqual({
      repo: 'bobbyrc/pimlo',
      branch: 'main',
      policy: 'supervised',
      setup: [{ name: 'npm ci', run: 'npm ci' }],
      checks: [
        { name: 'npm run lint', run: 'npm run lint' },
        { name: 'npm test', run: 'npm test' },
      ],
    });
    expect(addProjectRequest(result(), values({ name: 'web' })).name).toBe('web');
  });

  it('trims the branch and sends instructions only when they are not blank', () => {
    expect(addProjectRequest(result(), values({ branch: ' dev ', instructions: '  ' }))).toMatchObject({ branch: 'dev' });
    expect(addProjectRequest(result(), values({ instructions: 'Be brief.\n' }))).toMatchObject({ instructions: 'Be brief.\n' });
    expect(addProjectRequest(result(), values({ instructions: '  ' }))).not.toHaveProperty('instructions');
  });

  it('sends no setup or checks when none is chosen', () => {
    const body = addProjectRequest(result(), values({ none: true }));
    expect(body).not.toHaveProperty('setup');
    expect(body).not.toHaveProperty('checks');
  });

  it('drops blank commands and repeated ones', () => {
    const body = addProjectRequest(result(), values({ setup: ['  make  ', ''], checks: ['make', 'make test'] }));
    expect(body.setup).toEqual([{ name: 'make', run: 'make' }]);
    expect(body.checks).toEqual([
      { name: 'make', run: 'make' },
      { name: 'make test', run: 'make test' },
    ]);
  });
});

describe('firstTaskTitle and the name rule', () => {
  it('names the first task after the project', () => {
    expect(firstTaskTitle('pimlo')).toBe('Explain how pimlo is built and tested');
  });
  it('keeps the name rule the server uses', () => {
    expect(PROJECT_NAME.test('a.b_c-1')).toBe(true);
    expect(PROJECT_NAME.test('-a')).toBe(false);
  });
});
