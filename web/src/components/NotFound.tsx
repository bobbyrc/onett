import { Link } from 'react-router-dom';
import { ApiError } from '../api';
import { PageHeader } from './PageHeader';
import './NotFound.css';

/** True for the API's 404: an ID that was deleted or never existed. */
export function isNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

/** The page for a task, run, goal or trigger ID the server does not know: a heading, why, and a way back. */
export function NotFound({ what }: { what: 'task' | 'run' | 'goal' | 'trigger' }) {
  return (
    <>
      <PageHeader title={`${what[0].toUpperCase()}${what.slice(1)} not found`} />
      <p className="muted">{`No ${what} has this ID. It may have been deleted, or the link is wrong.`}</p>
      <p>
        <Link to={`/${what}s`} className="btn btn-secondary not-found-back">
          {`Back to ${what}s`}
        </Link>
      </p>
    </>
  );
}
