#!/usr/bin/env python3
"""M4 verification: branch → save → status → 409 on stale → commit → push."""
import json
import subprocess
import sys
import urllib.error
import urllib.request

BASE = 'http://127.0.0.1:8643/api/repos/trading-specs'
ROOT = '/home/flo/Projects/specquill'
BRANCH = 'feature/edit-test'
H = {'X-SpecQuill': '1', 'Content-Type': 'application/json'}


def call(method, url, body=None):
    req = urllib.request.Request(url, method=method,
                                 data=json.dumps(body).encode() if body is not None else None,
                                 headers=H)
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode())


def git(*args):
    return subprocess.run(['git', *args], capture_output=True, text=True, check=True).stdout.strip()


code, out = call('POST', f'{BASE}/branches', {'name': BRANCH, 'from': 'main'})
print('1. create branch:', code, out)

code, out = call('GET', f'{BASE}/files/specs/venue.md?ref={BRANCH}')
sha = out['sha']
content = out['content'].replace('segment MIC', 'segment MIC (v2)', 1)
print('2. loaded file, sha', sha[:10])

code, out = call('PUT', f'{BASE}/files/specs/venue.md?branch={BRANCH}', {'content': content, 'baseSha': sha})
print('3. save:', code, out)
assert code == 200

code, out = call('GET', f'{BASE}/status?branch={BRANCH}')
print('4. status:', code, out)
assert out['dirty'] == [{'path': 'specs/venue.md', 'state': 'M'}], out

code, out = call('PUT', f'{BASE}/files/specs/venue.md?branch={BRANCH}', {'content': 'x', 'baseSha': sha})
print('5. stale save:', code, out)
assert code == 409

code, out = call('POST', f'{BASE}/commit?branch={BRANCH}', {'message': 'venue: clarify segment MIC'})
print('6. commit:', code, out)
assert code == 200

log = git('-C', f'{ROOT}/data/runtime/repos/trading-specs/worktrees/feature__edit-test',
          'log', '-1', '--format=%an <%ae> | committer %cn <%ce>')
print('7. log:', log)
assert log == 'Flo Dev <flo@dev.local> | committer specquill <specquill@dev.local>', log

code, out = call('POST', f'{BASE}/push?branch={BRANCH}')
print('8. push:', code, out)
assert code == 200

origin = git('-C', f'{ROOT}/data/origin/trading-specs.git', 'log', '-1', '--format=%s by %an', BRANCH)
print('9. origin:', origin)
assert origin == 'venue: clarify segment MIC by Flo Dev', origin

code, out = call('GET', f'{BASE}/status?branch={BRANCH}')
assert out['ahead'] == 0 and out['behind'] == 0, out
print('10. ahead/behind after push: 0/0')

code, out = call('PUT', 'http://127.0.0.1:8643/api/repos/regulations/files/regulations/dora.md?branch=main',
                 {'content': 'x', 'baseSha': ''})
print('11. write to readonly repo:', code, out)
assert code == 403

print('\nALL WRITE-PATH CHECKS PASSED')
