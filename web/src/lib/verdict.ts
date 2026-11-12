// The one sentence at the top of the Overview page: what the board says about the line right now.
import { ageWords, type Need } from './needs';

export interface Verdict {
  lamp: 'stop' | 'call' | 'run' | 'off';
  /** The headline, set in capitals by CSS. */
  head: string;
  /** The first words of the sentence, set bold. May be empty. */
  lead: string;
  /** The rest of the sentence. */
  rest: string;
}

export interface VerdictInput {
  /** How many projects are configured; undefined while loading. */
  projects: number | undefined;
  /** Whether the reader is an owner (only owners see the setup steps). */
  owner: boolean;
  needs: Need[];
  activeRuns: number;
  paused: boolean;
  now: number;
  /** True when the problems or the approvals could not be loaded. */
  failed: boolean;
}

function words(value: string): string {
  return value.replace(/_/g, ' ');
}

function capFirst(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

// A title longer than 100 characters is cut to 99 and an ellipsis, so it cannot bury the sentence.
function short(text: string): string {
  return text.length > 100 ? `${text.slice(0, 99)}…` : text;
}

// A sentence ends in a full stop unless its own text already ends one; empty text stays empty.
function sentence(text: string): string {
  if (text === '') return '';
  return /[.!?…]$/.test(text) ? text : `${text}.`;
}

export function verdictFor(input: VerdictInput): Verdict {
  const { projects, owner, needs, activeRuns, paused, now, failed } = input;
  if (projects === undefined) return { lamp: 'off', head: 'Loading…', lead: '', rest: '' };
  if (projects === 0) {
    return {
      lamp: 'off',
      head: 'Not set up yet',
      lead: '',
      rest: owner
        ? 'onett has no project to work on. Start with step 1 below. Each project you add gets a row here: what needs you on the left, what is running on the right.'
        : 'onett has no project to work on. An owner can add one on the Projects page.',
    };
  }
  if (failed) {
    return { lamp: 'off', head: 'Cannot tell what needs you', lead: '', rest: 'The list did not load. The error is shown below.' };
  }

  const count = needs.length;
  if (count === 0) {
    if (paused) return { lamp: 'off', head: 'Nothing needs you', lead: '', rest: 'The line is paused: no new runs start.' };
    if (activeRuns > 0) {
      return {
        lamp: 'run',
        head: 'Nothing needs you',
        lead: '',
        rest: activeRuns === 1 ? '1 run is active.' : `${activeRuns} runs are active.`,
      };
    }
    return { lamp: 'off', head: 'Nothing needs you', lead: '', rest: 'Nothing is running.' };
  }

  const top = needs[0];
  const project = top.project;
  const age = ageWords(now - Date.parse(top.since));
  let lead: string;
  let start: string;
  let head = count === 1 ? '1 thing needs you' : `${count} things need you`;

  if (top.kind === 'approval') {
    const approval = top.approval;
    lead = `${capFirst(words(approval.kind))} approval on ${project}, asked ${age} ago:`;
    start = sentence(short(approval.title));
  } else {
    const problem = top.problem;
    if (problem.kind === 'blocked_tasks') {
      if (problem.count >= 2) {
        head = `Work is stopped on ${project}`;
        lead = `${problem.count} tasks on ${project} are blocked:`;
        start = sentence(short(problem.title));
      } else {
        lead = `A task on ${project} has been blocked for ${age}:`;
        start = sentence(short(problem.tasks?.[0]?.title ?? problem.title));
      }
    } else if (problem.kind === 'failing_loops') {
      const loops = problem.loops?.length ?? problem.count;
      lead =
        loops === 1
          ? `1 loop on ${project} has been failing for ${age}.`
          : `${loops} loops on ${project} have been failing for ${age}.`;
      start = '';
    } else {
      lead = `A goal on ${project} is blocked:`;
      start = sentence(short(problem.title));
    }
  }

  const more = count >= 2 ? `Start there. ${count - 1 === 1 ? '1 more thing follows.' : `${count - 1} more things follow.`}` : '';
  return { lamp: top.lamp, head, lead, rest: [start, more].filter((part) => part !== '').join(' ') };
}
