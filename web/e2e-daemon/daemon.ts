// Starts a real `onett serve` for the daemon journey: a temp HOME (so the config, secrets,
// database and clones all land in a throwaway directory), a port the OS picked, and a stub
// ntfy server that records what it is sent. Nothing here touches the real ~/.config or
// ~/.local/share.
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import http from 'node:http';
import net from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

export interface Daemon {
  /** The root of everything the run created. */
  root: string;
  home: string;
  configPath: string;
  secretsPath: string;
  /** The local git repository the test adds as a project. */
  repoDir: string;
  /** The address the daemon listens on. */
  url: string;
  /** The address `onett open` gives while no account exists: `url` plus the setup code after `#`. */
  setupUrl: string;
  /** What serve has printed so far (stdout and stderr), for failure messages. */
  log: () => string;
  stop: () => Promise<void>;
}

/** A free TCP port on loopback, chosen by the OS. */
export function freePort(): Promise<number> {
  return new Promise((resolvePort, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address() as net.AddressInfo;
      srv.close(() => resolvePort(port));
    });
  });
}

const BINARY_ENV = 'ONETT_E2E_BINARY';

/** The onett binary: the one named by $ONETT_E2E_BINARY (CI builds it first), else built here. */
function binary(root: string): string {
  const prebuilt = process.env[BINARY_ENV];
  if (prebuilt) return resolve(prebuilt);
  const out = join(root, 'onett');
  execFileSync('go', ['build', '-o', out, './cmd/onett'], { cwd: resolve(process.cwd(), '..'), stdio: 'inherit' });
  return out;
}

/** An environment with only what serve needs; none of the runner's own credentials. */
function cleanEnv(home: string): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { HOME: home, PATH: process.env.PATH, LANG: 'C.UTF-8' };
  for (const key of ['GOPATH', 'GOCACHE', 'GOFLAGS', 'GOPROXY', 'TMPDIR']) {
    if (process.env[key]) env[key] = process.env[key];
  }
  return env;
}

export async function startDaemon(): Promise<Daemon> {
  const root = mkdtempSync(join(tmpdir(), 'onett-e2e-'));
  const home = join(root, 'home');
  const repoDir = join(root, 'widgets');
  const configPath = join(home, '.config', 'onett', 'onett.yaml');
  mkdirSync(join(home, '.config', 'onett'), { recursive: true, mode: 0o700 });
  mkdirSync(repoDir);

  // A real repository with one commit, so the project path is a valid clone.
  const git = (...args: string[]) =>
    execFileSync('git', ['-c', 'user.name=e2e', '-c', 'user.email=e2e@example.invalid', ...args], { cwd: repoDir, stdio: 'ignore' });
  git('init', '-q', '-b', 'main');
  git('commit', '-q', '--allow-empty', '-m', 'init');

  // serve writes the starter config only when no file exists, and the starter listens on the
  // fixed port 7777. A one-line file that sets just the address gives the run its own port;
  // everything else still comes from the defaults, as for a user who only changed the port.
  const port = await freePort();
  writeFileSync(configPath, `server:\n  addr: 127.0.0.1:${port}\n`, { mode: 0o600 });

  const bin = binary(root);
  let output = '';
  const child: ChildProcess = spawn(bin, ['serve'], { cwd: root, env: cleanEnv(home), stdio: ['ignore', 'pipe', 'pipe'] });
  const collect = (chunk: Buffer) => (output += chunk.toString());
  child.stdout?.on('data', collect);
  child.stderr?.on('data', collect);
  const exited = new Promise<number | null>((done) => child.once('exit', done));

  const stop = async () => {
    if (child.exitCode === null && child.signalCode === null) {
      child.kill('SIGTERM');
      const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
      await exited;
      clearTimeout(timer);
    }
    rmSync(root, { recursive: true, force: true });
  };

  // The first-run line: serve prints it once the database is open, before it listens.
  const line = /No owner account yet: run `onett open`/;
  const deadline = Date.now() + 60_000;
  let ready = false;
  while (Date.now() < deadline) {
    if (line.test(output)) {
      ready = true;
      break;
    }
    if (child.exitCode !== null) {
      await stop();
      throw new Error(`onett serve exited early:\n${output}`);
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  if (!ready) {
    const seen = output;
    await stop();
    throw new Error(`onett serve never printed its first-run line:\n${seen}`);
  }
  const url = `http://127.0.0.1:${port}`;
  await waitListening(port);
  // The setup link, got the way a person gets it: `onett open`. PATH names a directory that does
  // not exist, so no browser opener is found and the command prints the address instead of
  // opening a window on the machine that runs the test.
  const opened = execFileSync(bin, ['open'], {
    cwd: root,
    env: { ...cleanEnv(home), PATH: join(root, 'no-browser-opener') },
    encoding: 'utf8',
  });
  const setupUrl = /Open this URL:\s*(\S+#setup=[0-9a-f]+)/.exec(opened)?.[1];
  if (!setupUrl) {
    await stop();
    throw new Error(`onett open printed no setup link:\n${opened}`);
  }
  return {
    root,
    home,
    configPath,
    secretsPath: join(home, '.config', 'onett', 'secrets.env'),
    repoDir,
    url,
    setupUrl,
    log: () => output,
    stop,
  };
}

/** Waits until the port accepts connections (serve prints its line just before it listens). */
async function waitListening(port: number): Promise<void> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const ok = await new Promise<boolean>((done) => {
      const s = net.connect(port, '127.0.0.1');
      s.once('connect', () => (s.destroy(), done(true)));
      s.once('error', () => done(false));
    });
    if (ok) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`nothing listened on port ${port}`);
}

export interface RecordedRequest {
  method: string;
  path: string;
  headers: http.IncomingHttpHeaders;
  body: string;
}

export interface Stub {
  url: string;
  requests: RecordedRequest[];
  close: () => Promise<void>;
}

/** A local HTTP server that answers 200 to everything and records each request (stands in for ntfy). */
export async function startStub(): Promise<Stub> {
  const requests: RecordedRequest[] = [];
  const server = http.createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => {
      requests.push({ method: req.method ?? '', path: req.url ?? '', headers: req.headers, body: Buffer.concat(chunks).toString() });
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end('{}');
    });
  });
  await new Promise<void>((ok) => server.listen(0, '127.0.0.1', ok));
  const { port } = server.address() as net.AddressInfo;
  return {
    url: `http://127.0.0.1:${port}`,
    requests,
    close: () => new Promise<void>((done) => (server.closeAllConnections(), server.close(() => done()))),
  };
}
