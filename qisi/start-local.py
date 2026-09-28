#!/usr/bin/env python3
"""Start the built console on loopback only; never expose first-run setup."""
import json
import os
from pathlib import Path
import secrets

root = Path(__file__).resolve().parent.parent
state = root / '.local-tests'
state.mkdir(mode=0o700, exist_ok=True)
config = state / 'local-runtime.json'
if not config.exists():
    fd = os.open(config, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as handle:
        json.dump({'SESSION_SECRET': secrets.token_hex(32)}, handle)
env = os.environ.copy()
env.update(json.loads(config.read_text()))
env.update({'BIND_ADDRESS': '127.0.0.1', 'PORT': '4319', 'PUBLIC_BASE_PATH': '/api-service',
            'SQLITE_PATH': str(state / 'qisi.db'), 'TRUSTED_PROXIES': 'none',
            'SESSION_COOKIE_SECURE': 'false', 'SESSION_COOKIE_TRUSTED_URL': ''})
os.umask(0o077)
os.chdir(root)
print('qisi API: http://127.0.0.1:4319/api-service/', flush=True)
os.execve(state / 'qisi-api', [str(state / 'qisi-api')], env)
