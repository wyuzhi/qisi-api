#!/usr/bin/env node
// Initialize the production database through a temporary loopback-only server.
// This script never creates generation tasks, payment orders or paid channels.
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, writeFile, stat, open } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import net from 'node:net';

const root = fileURLToPath(new URL('../', import.meta.url));
const localCheck = process.argv.includes('--local-check');
const state = path.join(root, '.local-tests');
await mkdir(state, { recursive: true, mode: 0o700 });
const configPath = path.join(state, localCheck ? `private-check-${Date.now()}.json` : 'production-runtime.json');
let config;
try {
  const info = await stat(configPath);
  if (info.mode & 0o077) throw new Error('Runtime file must be readable only by its owner (chmod 600).');
  try { config = JSON.parse(await readFile(configPath, 'utf8')); }
  catch { throw new Error('The private runtime file is not valid JSON; edit it locally.'); }
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
  config = {
    SQL_DSN: '', SESSION_SECRET: randomBytes(32).toString('hex'),
    admin: { username: 'qisiadmin', password: randomBytes(24).toString('base64url') },
  };
  await writeFile(configPath, JSON.stringify(config, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
}
if (process.argv.includes('--prepare')) {
  console.log(`Private runtime file: ${configPath}`);
  console.log('Fill SQL_DSN locally with the Supabase session-pooler connection string and TLS enabled. Do not share the file.');
  process.exit(0);
}
if (!localCheck) {
  let dsn;
  try { dsn = new URL(config.SQL_DSN); } catch { throw new Error('Fill SQL_DSN in the private runtime file first.'); }
  if (!['postgres:', 'postgresql:'].includes(dsn.protocol) || dsn.port !== '5432' ||
      !['require', 'verify-ca', 'verify-full'].includes(dsn.searchParams.get('sslmode'))) {
    throw new Error('Use the PostgreSQL session pooler on port 5432 with sslmode=require or stricter.');
  }
}
if (typeof config.SESSION_SECRET !== 'string' || config.SESSION_SECRET.length < 32 ||
    typeof config.admin?.password !== 'string' || config.admin.password.length < 15) {
  throw new Error('Private configuration is incomplete.');
}
const base = 'http://127.0.0.1:4320/api-service';
// Refuse to configure an unrelated process already on the initialization port.
await new Promise((resolve, reject) => {
  const probe = net.createServer();
  probe.once('error', () => reject(new Error('Cannot bind initialization port 4320; stop its existing process first.')));
  probe.listen(4320, '127.0.0.1', () => probe.close(resolve));
});
const logPath = configPath.replace(/\.json$/, '.log');
const log = await open(logPath, 'a', 0o600);
const binary = path.join(state, 'qisi-api-release');
const workingDirectory = configPath.replace(/\.json$/, '-work');
await mkdir(workingDirectory, { recursive: true, mode: 0o700 });
const child = spawn(binary, ['--log-dir', ''], {
  // Go loads .env relative to cwd. Keep unrelated preview settings out.
  cwd: workingDirectory,
  env: {
    PATH: process.env.PATH || '', HOME: process.env.HOME || '',
    SQL_DSN: localCheck ? '' : config.SQL_DSN, LOG_SQL_DSN: '',
    SQLITE_PATH: configPath.replace(/\.json$/, '.db'), SESSION_SECRET: config.SESSION_SECRET,
    CRYPTO_SECRET: config.SESSION_SECRET, REDIS_CONN_STRING: '', NODE_TYPE: 'master',
    ENABLE_PPROF: 'false', DEBUG: 'false', SQL_MAX_IDLE_CONNS: '2', SQL_MAX_OPEN_CONNS: '10', SQL_MAX_LIFETIME: '60',
    BIND_ADDRESS: '127.0.0.1', PORT: '4320', PUBLIC_BASE_PATH: '/api-service',
    SESSION_COOKIE_SECURE: 'false', SESSION_COOKIE_TRUSTED_URL: '', TRUSTED_PROXIES: 'none',
    REGISTRATION_PASSWORD_MIN_LENGTH: '15', GENERATE_DEFAULT_TOKEN: 'false',
    CRITICAL_RATE_LIMIT_ENABLE: 'true',
  },
  stdio: ['ignore', log.fd, log.fd],
});
let spawnError;
child.on('error', error => { spawnError = error; });
let token = '';
let cookie = '';
async function request(method, route, payload) {
  if (spawnError || child.exitCode !== null || child.signalCode !== null) throw new Error('Private backend is no longer running.');
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers.Authorization = `Bearer ${token}`;
  if (cookie) headers.Cookie = cookie;
  const response = await fetch(base + route, {
    method, headers, redirect: 'error', signal: AbortSignal.timeout(30000),
    body: payload === undefined ? undefined : JSON.stringify(payload),
  });
  const setCookies = response.headers.getSetCookie();
  if (setCookies.length) cookie = setCookies.map(value => value.split(';')[0]).join('; ');
  const result = await response.json();
  if (!response.ok || result.success === false) throw new Error(`Setup request failed: ${route} (HTTP ${response.status}); inspect the private runtime log.`);
  return result;
}
try {
  let setup;
  for (let attempt = 0; attempt < 60; attempt++) {
    if (spawnError || child.exitCode !== null) throw new Error(`Backend failed to start; inspect ${logPath}`);
    try { setup = (await request('GET', '/api/setup')).data; break; } catch { await new Promise(resolve => setTimeout(resolve, 500)); }
  }
  if (!setup) throw new Error(`Backend did not become ready; inspect ${logPath}`);
  if (setup.status) throw new Error('Database already initialized; existing production settings were not changed.');
  if (setup.root_init) throw new Error('Existing administrator found; finish initialization manually.');
  await request('POST', '/api/setup', { ...config.admin, confirmPassword: config.admin.password, SelfUseModeEnabled: false, DemoSiteEnabled: false });
  const login = (await request('POST', '/api/user/login', config.admin)).data;
  token = login.access_token;
  const publicURL = 'https://cheeser.link/api-service';
  const options = {
    SystemName: 'qisi API', Logo: '/api-service/qisi-logo.jpg', ServerAddress: publicURL,
    'general_setting.docs_link': publicURL + '/qisi-guide.html', 'general_setting.quota_display_type': 'CNY',
    RegisterEnabled: 'true', PasswordRegisterEnabled: 'true', PasswordLoginEnabled: 'true', EmailVerificationEnabled: 'false',
    QuotaForNewUser: '0', QuotaForInviter: '0', QuotaForInvitee: '0', RetryTimes: '0',
    HeaderNavModules: JSON.stringify({ home: true, console: true, pricing: { enabled: true, requireAuth: false }, rankings: { enabled: false, requireAuth: false }, docs: true, about: true }),
    Notice: 'qisi API 试运行：支持注册和密钥管理。在线充值和收费生成暂未开放。',
  };
  for (const [key, value] of Object.entries(options)) await request('PUT', '/api/option/', { key, value });
  await request('POST', '/api/plugin/task', { source: await readFile(path.join(root, 'qisi/likeai.plugin.js'), 'utf8'), enabled: true, remark: 'LikeAI 适配器；收费渠道尚未启用' });
  await request('POST', '/api/user/manage', { id: login.user.id, action: 'add_quota', mode: 'override', value: 0 });
  const status = (await request('GET', '/api/status')).data;
  if (!status.register_enabled || !status.password_register_enabled || status.registration_password_min_length !== 15) throw new Error('Registration configuration did not pass verification.');
  console.log(localCheck ? 'Disposable SQLite initialization check passed.' : 'Production database initialized privately.');
  console.log(`Administrator and runtime configuration: ${configPath} (owner-only).`);
  console.log('No paid channels, payment gateway or generation tasks were created.');
} finally {
  if (token) { try { await request('POST', '/api/user/auth/logout', {}); } catch { /* local process is stopped below */ } }
  child.kill('SIGTERM');
  await log.close();
}
