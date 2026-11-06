// A free loopback port for the `vite preview` server a Playwright config starts, so two runs
// (for example in parallel worktrees) never fight over one fixed port.
//
// Set the variable named by `envName` to pin a port. Otherwise the OS picks a free one and it is
// written back to that variable: Playwright loads the config again in every worker process, and
// the workers inherit the variable, so they all see the same port instead of choosing their own.
import net from 'node:net';

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      srv.close(() => (addr && typeof addr === 'object' ? resolve(addr.port) : reject(new Error('no port'))));
    });
  });
}

export async function previewPort(envName: string): Promise<number> {
  const pinned = Number(process.env[envName]);
  if (Number.isInteger(pinned) && pinned > 0) return pinned;
  const port = await freePort();
  process.env[envName] = String(port);
  return port;
}
