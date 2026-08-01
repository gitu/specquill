#!/usr/bin/env python3
"""Tiny OpenAI-compatible /v1/chat/completions mock for dev & e2e.

Streams a canned grounded answer for chat; returns a canned edit-JSON for the
speccy draft prompt (fixes the RTS 22 timestamp drift in the demo repo).
"""
import json
import re
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = 8991

DRAFT_REPLY = json.dumps({
    "summary": ("Updated the trade.executionTimestamp lineage to microsecond "
                "precision per RTS 22 §2: the OMS transform now emits ISO-8601 "
                "with μs and the drifted rows are marked ok in both the mapping "
                "and the spec's data-mapping table."),
    "edits": [
        {
            "path": "data-mappings/trade.md",
            "search": "| 1 | oms.exec_time   | trade.executionTimestamp   | to ISO-8601, μs prec. | data  | ⚠ drift |",
            "replace": "| 1 | oms.exec_time   | trade.executionTimestamp   | to ISO-8601, μs prec. | data  | ✓ ok    |",
        },
        {
            "path": "specs/txn-report.md",
            "search": "| oms.exec_time | trade.executionTimestamp | ISO-8601 μs | ⚠ drift |",
            "replace": "| oms.exec_time | trade.executionTimestamp | ISO-8601 μs | ✓ ok    |",
        },
    ],
})


class Handler(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *args):
        pass

    def do_POST(self):
        if not self.path.endswith('/chat/completions'):
            self.send_error(404)
            return
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        system = next((m['content'] for m in body['messages'] if m['role'] == 'system'), '')
        user = next((m['content'] for m in reversed(body['messages']) if m['role'] == 'user'), '')

        # deterministic tool behavior for the chat-tools e2e: the user message
        # carries a directive, the mock emits the matching tool_calls round
        # (arguments fragmented on purpose to exercise accumulation)
        tool_call = None
        last = body['messages'][-1]
        if 'specquill linker' in system:
            # linker: one canned missing link that holds in the demo fixture
            # (venue.md implements REQ-051/070 but not REQ-063)
            reply = json.dumps({'proposals': [{
                'from': 'specs/venue.md',
                'field': 'implements',
                'to': 'requirements/REQ-063.md',
                'reason': '(mock) the venue spec realizes partial-fill venue '
                          'resolution but does not declare REQ-063.',
            }]})
        elif 'coverage-gap auditor' in system:
            # gap sweep: one canned uncovered capability, evidence quoting the
            # demo regulations source VERBATIM (unverified quotes get dropped)
            reply = json.dumps({'findings': [{
                'anchor': 'regulations/gdpr.md#art-5-storage-limitation',
                'severity': 'medium',
                'title': '(mock) GDPR storage limitation has no requirement',
                'detail': 'The regulations source constrains retention but no '
                          'workspace document covers a retention requirement for reports.',
                'suggestedPath': 'requirements/REQ-gdpr-retention.md',
                'sourcePaths': ['regulations/gdpr.md'],
                'evidence': [{'path': 'regulations/gdpr.md',
                              'quote': 'kept for no longer than is necessary'}],
            }]})
        elif 'requirements reverse-engineer' in system:
            # reverse engineering: draft the missing requirement document
            reply = json.dumps({
                'path': 'requirements/REQ-gdpr-retention.md',
                'content': ('---\nid: REQ-gdpr-retention\ntitle: Report Data Retention\n'
                            'type: requirement\nstatus: draft\npriority: should\n'
                            'drivers: [regulations/gdpr.md]\n---\n\n'
                            '# Report Data Retention\n\n'
                            '(mock) Report data shall be kept for no longer than is necessary '
                            'for the reporting purpose.\n\n'
                            'Derived from ~regulations/gdpr.md (Art. 5 storage limitation).\n'),
            })
        elif 'source-drift auditor' in system:
            # drift check: one canned finding whose evidence quotes the demo
            # regulations source VERBATIM (the server drops unverified quotes)
            anchor = re.search(r'^id:\s*(\S+)', user, re.M)
            reply = json.dumps({'findings': [{
                'anchor': anchor.group(1) if anchor else 'REQ-042',
                'source': 'regulations',
                'kind': 'outdated-requirement',
                'severity': 'high',
                'title': '(mock) timestamp precision drifted vs RTS 22 amendment',
                'detail': 'The 2026-06 amendment requires microsecond execution '
                          'timestamps; this document still allows coarser precision.',
                'sourcePaths': ['regulations/mifid-ii.md'],
                'evidence': [{'path': 'regulations/mifid-ii.md',
                              'quote': 'reported to **microsecond** precision'}],
            }]})
        elif body.get('tools') and last['role'] == 'tool':
            name = ''
            for m in reversed(body['messages']):
                if m['role'] == 'assistant' and m.get('tool_calls'):
                    name = next((tc['function']['name'] for tc in m['tool_calls']
                                 if tc['id'] == last.get('tool_call_id')), '')
                    break
            if name == 'ask_user' and 'READFIRST' in last['content']:
                # answered question → the model consults a file next
                tool_call = {'name': 'read_file', 'arguments': json.dumps({'path': 'specs/txn-report.md'})}
                reply = ''
            elif name == 'ask_user':
                reply = f"(mock) noted: {last['content']}."
            elif name == 'read_file':
                # after reading, the model asks a follow-up — the exact chain
                # reported from gpt-5.x (answer → read_file → next question)
                tool_call = {'name': 'ask_user',
                             'arguments': json.dumps({'question': 'Follow-up question?', 'options': ['gamma', 'delta']})}
                reply = 'Read it; one point is still open.'
            elif last['content'].startswith('ERROR'):
                reply = f"(mock) the edit failed: {last['content']}"
            else:
                reply = "(mock) applied the edit as an uncommitted draft — review it in the changes drawer."
        elif body.get('tools') and (m := re.search(r'EDIT (\S+) REPLACE "([^"]+)" WITH "([^"]+)"', user)):
            tool_call = {'name': 'edit_file',
                         'arguments': json.dumps({'path': m.group(1), 'search': m.group(2), 'replace': m.group(3)})}
            reply = ''
        elif body.get('tools') and (m := re.search(r'MOVE (\S+) TO (\S+)', user)):
            tool_call = {'name': 'move_file',
                         'arguments': json.dumps({'from': m.group(1), 'to': m.group(2)})}
            reply = ''
        elif body.get('tools') and (m := re.search(r'DELETE (\S+)', user)):
            tool_call = {'name': 'delete_file', 'arguments': json.dumps({'path': m.group(1)})}
            reply = ''
        elif body.get('tools') and (m := re.search(r'DRAW (\S+)', user)):
            scene = json.dumps({'elements': [
                {'type': 'rectangle', 'x': 10, 'y': 10, 'width': 170, 'height': 60},
                {'type': 'text', 'x': 40, 'y': 32, 'text': 'mock box'},
            ]})
            tool_call = {'name': 'draw_sketch', 'arguments': json.dumps({'path': m.group(1), 'scene': scene})}
            reply = ''
        elif body.get('tools') and 'READFIRST' in user:
            tool_call = {'name': 'read_file', 'arguments': json.dumps({'path': 'specs/txn-report.md'})}
            reply = ''
        elif body.get('tools') and 'ASKME' in user:
            # content BEFORE the tool call, like real providers stream it —
            # the preface must reach the question card
            tool_call = {'name': 'ask_user',
                         'arguments': json.dumps({'question': 'Which option do you want?', 'options': ['alpha', 'beta']})}
            reply = 'I checked the spec; one point is unresolved.'
        elif 'word title' in system:
            reply = 'Mock Chat Title'
        elif 'Reply with ONLY a JSON object' in system:
            reply = DRAFT_REPLY
        else:
            # workspace files head `## <path>`; grounded reference files head
            # `## ~<source>/<path>` — count them apart so the reply reflects what
            # the server actually put in the prompt (grant-gated grounding, P4).
            n_files = len(re.findall(r'^## (?!~)\S+\.(?:md|ya?ml|json|mermaid)$', system, re.M))
            sources = sorted(set(re.findall(r'^# Reference source ~(\S+)', system, re.M)))
            focus = re.search(r'currently viewing: (\S+)', system)
            reply = (f"(mock) I am grounded on {n_files} workspace files"
                     + (f", focused on {focus.group(1)}" if focus else '')
                     + (f", plus grounded sources: {', '.join(sources)}" if sources else '')
                     + f". You asked: “{user[:120]}” — in the demo workspace the "
                       "RTS 22 amendment drives REQ-042 and the drifted mapping is "
                       "trade.executionTimestamp (see data-mappings/trade.md).")

        if body.get('stream'):
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Transfer-Encoding', 'chunked')
            self.end_headers()
            try:
                # content first, then tool_calls — the order real providers use
                for i in range(0, len(reply), 24):
                    chunk = json.dumps({'choices': [{'delta': {'content': reply[i:i + 24]}}]})
                    self._chunk(f"data: {chunk}\n\n")
                    time.sleep(0.01)
                if tool_call:
                    args = tool_call['arguments']
                    half = len(args) // 2
                    frags = [
                        {'index': 0, 'id': 'call_mock_1', 'type': 'function',
                         'function': {'name': tool_call['name'], 'arguments': ''}},
                        {'index': 0, 'function': {'arguments': args[:half]}},
                        {'index': 0, 'function': {'arguments': args[half:]}},
                    ]
                    for f in frags:
                        chunk = json.dumps({'choices': [{'delta': {'tool_calls': [f]}}]})
                        self._chunk(f"data: {chunk}\n\n")
                        time.sleep(0.01)
                self._chunk("data: [DONE]\n\n")
                self._chunk('')
            except (BrokenPipeError, ConnectionResetError):
                pass  # client hung up mid-stream — not fatal to the server
        else:
            raw = json.dumps({'choices': [{'message': {'role': 'assistant', 'content': reply}}]}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

    def _chunk(self, s):
        data = s.encode()
        self.wfile.write(f"{len(data):x}\r\n".encode() + data + b"\r\n")


if __name__ == '__main__':
    print(f"mock LLM on :{PORT}")
    HTTPServer(('127.0.0.1', PORT), Handler).serve_forever()
