#!/usr/bin/env python3
"""M7 verification: edit → commit → PR (dirty prompt) → comment → approve →
merge → target reflects change; conflict fixture blocked with file list."""
import json
import subprocess
import urllib.error
import urllib.request

BASE = 'http://127.0.0.1:8643/api/repos/trading-specs'
ROOT = '/home/flo/Projects/specquill'
H = {'X-SpecQuill': '1', 'Content-Type': 'application/json'}


def call(method, url, body=None):
    req = urllib.request.Request(url, method=method,
                                 data=json.dumps(body).encode() if body is not None else None, headers=H)
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode())


def git(*args):
    return subprocess.run(['git', *args], capture_output=True, text=True, check=True).stdout.strip()


BR = 'feature/pr-flow'
call('POST', f'{BASE}/branches', {'name': BR, 'from': 'main'})

# edit REQ-063 coverage on the branch
code, f = call('GET', f'{BASE}/files/requirements/REQ-063.md?ref={BR}')
content = f['content'].replace('coverage: 0.68', 'coverage: 0.75')
code, _ = call('PUT', f'{BASE}/files/requirements/REQ-063.md?branch={BR}', {'content': content, 'baseSha': f['sha']})
assert code == 200

# PR creation with dirty worktree → 409 code=dirty (commit prompt)
code, out = call('POST', f'{BASE}/prs', {'title': 'Raise REQ-063 coverage', 'source': BR})
print('1. PR on dirty branch:', code, out.get('code'), out.get('dirty'))
assert code == 409 and out['code'] == 'dirty'

code, _ = call('POST', f'{BASE}/commit?branch={BR}', {'message': 'REQ-063: coverage 0.75'})
assert code == 200
code, pr = call('POST', f'{BASE}/prs', {'title': 'Raise REQ-063 coverage', 'body': 'Desk confirmed new tests.', 'source': BR})
print('2. PR created:', code, '#' + str(pr['number']), 'mergeable:', pr['mergeable'])
assert code == 200 and pr['mergeable'] is True
n = pr['number']

# diff has the coverage change
code, diff = call('GET', f'{BASE}/prs/{n}/diff')
file0 = diff['files'][0]
print('3. diff:', file0['path'], f"+{file0['additions']} -{file0['deletions']}")
assert file0['path'] == 'requirements/REQ-063.md'
assert any('coverage: 0.75' in ln['text'] for h in file0['hunks'] for ln in h['lines'] if ln['op'] == '+')

# inline + general comments
code, _ = call('POST', f'{BASE}/prs/{n}/comments', {'body': 'Confirm the desk sign-off?', 'path': 'requirements/REQ-063.md', 'line': 16})
code, _ = call('POST', f'{BASE}/prs/{n}/comments', {'body': 'LGTM overall.'})
code, comments = call('GET', f'{BASE}/prs/{n}/comments')
print('4. comments:', [(c['body'], c.get('filePath'), c['outdated']) for c in comments])
assert len(comments) == 2 and comments[0]['filePath'] == 'requirements/REQ-063.md' and not comments[0]['outdated']

# approve (pinned to head)
code, out = call('POST', f'{BASE}/prs/{n}/approve')
print('5. approve:', code, [(a['user']['name'], a['current']) for a in out['approvals']])
assert out['approvals'][0]['current'] is True

# merge
code, out = call('POST', f'{BASE}/prs/{n}/merge', {'strategy': 'merge'})
print('6. merge:', code, out)
assert code == 200
merged = out['mergedCommit']
log = git('-C', f'{ROOT}/data/runtime/repos/trading-specs/git', 'log', '-1', '--format=%an|%cn|%s', merged)
print('7. merge commit:', log)
assert log.startswith('Flo Dev|specquill|Merge PR #')

# main now carries the change (both in git and via the API/model path)
code, f = call('GET', f'{BASE}/files/requirements/REQ-063.md?ref=main')
assert 'coverage: 0.75' in f['content']
code, snap = call('GET', f'{BASE}/snapshot?ref=main')
assert 'coverage: 0.75' in snap['files']['requirements/REQ-063.md']
print('8. main reflects merged change (file + snapshot)')

code, pr = call('GET', f'{BASE}/prs/{n}')
assert pr['state'] == 'merged'
print('9. PR state:', pr['state'])

# conflict fixture: two branches from the same base edit the same line; the
# first merges (moving protected main via PR), the second then conflicts
BR2 = 'feature/conflict'
BR3 = 'feature/conflict-winner'
for br, repl in ((BR2, 'XXXX'), (BR3, 'YYYY')):
    call('POST', f'{BASE}/branches', {'name': br, 'from': 'main'})
    code, f = call('GET', f'{BASE}/files/specs/venue.md?ref={br}')
    call('PUT', f'{BASE}/files/specs/venue.md?branch={br}', {'content': f['content'].replace('XOFF', repl), 'baseSha': f['sha']})
    call('POST', f'{BASE}/commit?branch={br}', {'message': f'{br} edit'})
code, prw = call('POST', f'{BASE}/prs', {'title': 'Winner venue edit', 'source': BR3})
code, out = call('POST', f"{BASE}/prs/{prw['number']}/merge", {})
assert code == 200, out
code, pr2 = call('POST', f'{BASE}/prs', {'title': 'Conflicting venue edit', 'source': BR2})
print('10. conflicting PR mergeable:', pr2['mergeable'], pr2.get('conflicts'))
assert pr2['mergeable'] is False and pr2['conflicts'] == ['specs/venue.md']
code, out = call('POST', f"{BASE}/prs/{pr2['number']}/merge", {})
print('11. merge blocked:', code, out)
assert code == 409 and out['conflicts'] == ['specs/venue.md']

print('\nALL PR-FLOW CHECKS PASSED')
