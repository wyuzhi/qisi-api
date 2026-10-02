#!/usr/bin/env python3
"""Initialize only the local preview, without making upstream generation calls."""
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import urllib.request

root = Path(__file__).resolve().parent.parent
base = 'http://127.0.0.1:4319/api-service'
credentials = root / '.local-tests' / 'admin.json'
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
token = None

def request(method, path, payload=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + path, method=method, headers=headers,
                                 data=json.dumps(payload).encode() if payload is not None else None)
    with client.open(req, timeout=30) as response:
        result = json.load(response)
    if result.get('success') is False:
        raise RuntimeError(path + ': ' + str(result.get('message', 'request failed')))
    return result

setup = request('GET', '/api/setup')['data']
if not setup['status']:
    if setup['root_init']:
        raise SystemExit('Existing root account found; finish setup manually.')
    if not credentials.exists():
        fd = os.open(credentials, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'w') as file:
            json.dump({'username': 'qisiadmin', 'password': secrets.token_urlsafe(24)}, file)
    account = json.loads(credentials.read_text())
    request('POST', '/api/setup', {**account, 'confirmPassword': account['password'],
                                 'SelfUseModeEnabled': False, 'DemoSiteEnabled': False})
else:
    raise SystemExit('Already initialized; existing settings were not overwritten.')
account = json.loads(credentials.read_text())
login = request('POST', '/api/user/login', account)['data']
token = login['access_token']
options = {
    'SystemName': 'qisi API',
    'HeaderNavModules': json.dumps({'home': True, 'console': True, 'pricing': {'enabled': True, 'requireAuth': False}, 'rankings': {'enabled': False, 'requireAuth': False}, 'docs': True, 'about': True}), 'Logo': '/api-service/qisi-logo.jpg',
    'ServerAddress': base, 'general_setting.docs_link': base + '/qisi-guide.html',
    'general_setting.quota_display_type': 'CNY', 'RetryTimes': '0',
    'RegisterEnabled': 'true', 'PasswordRegisterEnabled': 'true',
    'PasswordLoginEnabled': 'true', 'EmailVerificationEnabled': 'false', 'QuotaForNewUser': '0',
    'QuotaForInviter': '0', 'QuotaForInvitee': '0',
    'Notice': 'qisi API 本地准备版本。在线支付待开通，LikeAI 模型需核对成本并配置售价后启用。',
}
for key, value in options.items():
    request('PUT', '/api/option/', {'key': key, 'value': value})
request('POST', '/api/plugin/task', {'source': (root / 'qisi/likeai.plugin.js').read_text(),
                                   'enabled': True, 'remark': 'LikeAI 任务适配器；未配置价格，不启用付费渠道'})
request('POST', '/api/user/manage', {'id': login['user']['id'], 'action': 'add_quota', 'mode': 'override', 'value': 0})
request('POST', '/api/user/auth/logout', {})
print('Initialized qisi API. Admin credentials are in .local-tests/admin.json (mode 600).')
print('No payment gateway, paid channel, or artificial balance is enabled.')
