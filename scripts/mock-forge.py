#!/usr/bin/env python3
"""Tiny GitLab-API mock for forge-PAT dev & e2e.

Serves just what SpecQuill's PAT mode touches: /user (identity), the project
permission probe, and merge-request list/create. Any token listed in TOKENS
signs in; the mapped access level decides the deployment role (30 developer →
editor, 40 maintainer, 50 owner → admin).

Point a PAT-mode config at it:

    auth:
      forge: {kind: gitlab, base_url: http://127.0.0.1:8992}

and sign in with e.g. "tok-dev".
"""
import json
import re
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = 8992

TOKENS = {
    'tok-dev': {'id': 901, 'username': 'dev', 'name': 'Dev User', 'level': 30},
    'tok-maint': {'id': 902, 'username': 'maint', 'name': 'Maintainer User', 'level': 40},
    'tok-view': {'id': 903, 'username': 'view', 'name': 'Viewer User', 'level': 20},
}

open_mrs = {}  # source_branch -> mr dict
next_iid = [1]


class Handler(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *args):
        pass

    def _user(self):
        return TOKENS.get(self.headers.get('PRIVATE-TOKEN', ''))

    def _json(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        u = self._user()
        if not u:
            return self._json(401, {'message': '401 Unauthorized'})
        if self.path == '/api/v4/user':
            return self._json(200, {
                'id': u['id'], 'username': u['username'], 'name': u['name'],
                'email': u['username'] + '@mock.local', 'commit_email': '',
            })
        if re.search(r'/merge_requests\?', self.path):
            branch = re.search(r'source_branch=([^&]+)', self.path)
            key = branch.group(1) if branch else ''
            mr = open_mrs.get(key)
            return self._json(200, [mr] if mr else [])
        if '/api/v4/projects/' in self.path:
            return self._json(200, {'permissions': {
                'project_access': {'access_level': u['level']}, 'group_access': None,
            }})
        self._json(404, {'message': 'not mocked: ' + self.path})

    def do_POST(self):
        u = self._user()
        if not u:
            return self._json(401, {'message': '401 Unauthorized'})
        length = int(self.headers.get('Content-Length') or 0)
        payload = json.loads(self.rfile.read(length) or b'{}')
        if self.path.endswith('/merge_requests'):
            iid = next_iid[0]
            next_iid[0] += 1
            mr = {
                'iid': iid,
                'title': payload.get('title') or 'untitled',
                'state': 'opened',
                'web_url': 'http://127.0.0.1:%d/mr/%d' % (PORT, iid),
                'author': {'username': u['username']},
            }
            open_mrs[payload.get('source_branch', '')] = mr
            return self._json(201, mr)
        self._json(404, {'message': 'not mocked: ' + self.path})


if __name__ == '__main__':
    print('mock forge (gitlab) on :%d — tokens: %s' % (PORT, ', '.join(TOKENS)))
    HTTPServer(('127.0.0.1', PORT), Handler).serve_forever()
