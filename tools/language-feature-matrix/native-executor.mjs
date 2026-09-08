import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';

const OUTPUT_LIMIT = 1024 * 1024;
const COMPILE_TIMEOUT = 45_000;
const RUN_TIMEOUT = 6_000;
const extensions = { python: '.py', go: '.go', rust: '.rs', c: '.c', cpp: '.cpp', java: '.java', nim: '.nim' };

// Same fixture normalization as matrix-audit/audit.mjs. This compares a measured
// output to a fixture contract; it does not certify general language semantics.
export function matches(text, { kind, expected }) {
  if (typeof text !== 'string') return false;
  const value = text.trim().replace(/^\[1\]\s*/, '');
  if (kind === 'lines') return value.replace(/\r/g, '').replace(/^\[1\]\s*/gm, '') === expected;
  if (kind === 'string') return value === expected || value === JSON.stringify(expected);
  if (kind === 'boolean') {
    return ['true', '1', 'TRUE', 'True'].includes(value) === expected && /^(true|false|TRUE|FALSE|True|False|0|1)$/.test(value);
  }
  return value !== '' && Number.isFinite(Number(value)) && Math.abs(Number(value) - expected) < 1e-10;
}

/** Execute only generated validation programs; never builds the transpiler. */
export function createNativeExecutor(root) {
  root = path.resolve(root);
  const python = process.env.AUDIT_PYTHON || path.join(os.homedir(), '.cache', 'codex-runtimes', 'codex-primary-runtime', 'dependencies', 'python', 'python.exe');
  const javaHome = process.env.AUDIT_JAVA_HOME || path.join(process.env.ProgramFiles || 'C:\\Program Files', 'Android', 'Android Studio', 'jbr');
  const commands = {
    python,
    go: process.env.AUDIT_GO || 'go',
    rust: process.env.AUDIT_RUSTC || 'rustc',
    c: process.env.AUDIT_CC || 'gcc',
    cpp: process.env.AUDIT_CXX || 'g++',
    java: path.join(javaHome, 'bin', 'javac.exe'),
    nim: process.env.AUDIT_NIM || path.join(root, '.audit-cache', 'toolchains', 'nim-2.2.10', 'bin', 'nim.exe'),
  };
  const env = {
    ...process.env,
    GOCACHE: path.join(root, '.audit-cache', 'go-build'),
    PYTHONDONTWRITEBYTECODE: '1',
    PYTHONIOENCODING: 'utf-8',
  };
  const cache = new Map();

  function command(executable, args, cwd, timeout) {
    return new Promise(resolve => {
      const stdoutChunks = [], stderrChunks = [];
      let bytes = 0, settled = false, timer;
      let resourceReason = '';
      const child = spawn(executable, args, { cwd, env, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] });
      const finish = result => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        resolve({ ...result, stdout: Buffer.concat(stdoutChunks).toString('utf8'), stderr: Buffer.concat(stderrChunks).toString('utf8'), resourceReason, command: [executable, ...args] });
      };
      const stop = reason => {
        if (resourceReason || settled) return;
        resourceReason = reason;
        // On Windows a compiler can own subprocesses. Kill just this process tree,
        // not unrelated compilers or the user's application.
        if (process.platform === 'win32' && child.pid) {
          const killer = spawn('taskkill.exe', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore' });
          killer.on('error', () => child.kill());
        } else child.kill('SIGKILL');
        finish({ exit: null, limited: true });
      };
      timer = setTimeout(() => stop(`timeout after ${timeout} ms`), timeout);
      const append = stream => chunk => {
        if (settled) return;
        const remaining = Math.max(0, OUTPUT_LIMIT - bytes);
        const accepted = chunk.subarray(0, remaining);
        if (stream === 'stdout') stdoutChunks.push(accepted);
        else stderrChunks.push(accepted);
        bytes += chunk.length;
        if (bytes > OUTPUT_LIMIT) stop(`combined output exceeds ${OUTPUT_LIMIT} bytes`);
      };
      child.stdout.on('data', append('stdout'));
      child.stderr.on('data', append('stderr'));
      child.on('error', error => finish({ exit: null, unavailable: true, error: `${error.code || 'SPAWN'}: ${error.message}` }));
      child.on('close', (exit, signal) => finish({ exit, signal }));
      child.stdin.on('error', () => {});
      child.stdin.end();
    });
  }

  const indeterminate = detail => detail.limited || detail.unavailable || detail.exit === null;
  const reason = detail => detail.resourceReason || detail.error || detail.stderr || (detail.exit !== 0 ? `process exit ${detail.exit}${detail.signal ? ` (${detail.signal})` : ''}` : '');

  async function executeUncached(target, code, key) {
    if (!Object.hasOwn(commands, target)) {
      return { compile: 'UNKNOWN', run: 'UNKNOWN', stdout: '', stderr: '', reason: 'native target toolchain not configured' };
    }
    const directory = path.join(root, '.audit-cache', 'relay-native', key);
    await fs.mkdir(directory, { recursive: true });
    const file = path.join(directory, target === 'java' ? 'Main.java' : `program${extensions[target]}`);
    const program = path.join(directory, 'program.exe');
    await fs.writeFile(file, code, 'utf8');
    const args = {
      python: ['-c', 'import ast,sys; ast.parse(open(sys.argv[1], encoding="utf-8").read())', file],
      go: ['build', '-o', program, file],
      rust: ['-A', 'warnings', file, '-o', program],
      c: ['-std=c11', file, '-o', program],
      cpp: ['-std=c++17', file, '-o', program],
      java: ['-encoding', 'UTF-8', file],
      nim: ['c', '--cc:gcc', '--hints:off', '--warnings:off', '--colors:off', `--nimcache:${path.join(directory, 'nimcache')}`, `--out:${program}`, file],
    }[target];
    const compilation = await command(commands[target], args, root, COMPILE_TIMEOUT);
    if (compilation.exit !== 0) {
      return { compile: indeterminate(compilation) ? 'UNKNOWN' : 'FAIL', run: 'UNKNOWN', stdout: '', stderr: compilation.stderr, reason: reason(compilation), compile_detail: compilation };
    }
    const executable = target === 'python' ? python : target === 'java' ? path.join(javaHome, 'bin', 'java.exe') : program;
    const runArgs = target === 'python' ? [file] : target === 'java' ? ['-Dfile.encoding=UTF-8', '-Dstdout.encoding=UTF-8', '-cp', directory, 'Main'] : [];
    const execution = await command(executable, runArgs, directory, RUN_TIMEOUT);
    return {
      compile: 'PASS',
      run: indeterminate(execution) ? 'UNKNOWN' : execution.exit === 0 ? 'PASS' : 'FAIL',
      stdout: execution.stdout,
      stderr: execution.stderr,
      reason: reason(execution),
      compile_detail: compilation,
      run_detail: execution,
    };
  }

  // Promise memoization also deduplicates simultaneous requests. Disk artifacts
  // are evidence only: cached results are not reused across compiler sessions.
  async function execute(target, code) {
    if (typeof target !== 'string' || typeof code !== 'string') {
      return { compile: 'UNKNOWN', run: 'UNKNOWN', stdout: '', stderr: '', reason: 'target and code must be strings' };
    }
    const key = createHash('sha256').update(target).update('\0').update(code).digest('hex');
    if (!cache.has(key)) {
      cache.set(key, executeUncached(target, code, key).catch(error => ({
        compile: 'UNKNOWN', run: 'UNKNOWN', stdout: '', stderr: '', reason: `validation infrastructure: ${error.message}`,
      })));
    }
    return cache.get(key);
  }

  return execute;
}
