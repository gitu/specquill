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


def wizard_reply(system, messages):
    """Canned JSON for the guided-authoring stages (server/internal/ai/wizard.go).

    Each stage is identified by a phrase unique to its prompt contract. The
    interview answers differently once the transcript has an answer in it, so
    the e2e can walk intent → questions → rubric filling up → ready-to-draft.
    """
    if 'recommendation' in system and '"matches"' in system:
        return json.dumps({
            'matches': [{
                'path': 'specs/txn-report.md',
                'title': 'Transaction reporting',
                'relation': 'overlaps',
                'reason': '(mock) already specifies the reporting timestamps this intent touches.',
            }],
            'recommendation': 'new',
        })
    if 'readyToDraft' in system:
        answered = any(m['role'] == 'user' and not str(m['content']).startswith('Intent:') for m in messages)
        if answered:
            return json.dumps({
                'reply': '(mock) Understood — that is enough to draft.',
                'questions': [],
                'rubric': [{'criterion': 'Retention period stated', 'met': True},
                           {'criterion': 'Scope named', 'met': True}],
                'readyToDraft': True,
            })
        return json.dumps({
            'reply': '(mock) specs/txn-report.md already covers the reporting flow. Two things are open.',
            'questions': [
                {'question': 'Does this replace the existing window?',
                 'options': ['Replaces it', 'Runs alongside for existing records']},
                # no options: exercises the free-text-only card
                {'question': 'Which records are in scope?'},
            ],
            'rubric': [{'criterion': 'Retention period stated', 'met': False},
                       {'criterion': 'Scope named', 'met': False}],
            'readyToDraft': False,
        })
    if 'a short imperative document title' in system:
        # compose answers in MARKDOWN, not JSON — section bodies are prose and
        # a JSON envelope made real models fail on escaping. Echo the requested
        # outline so the test sees the server's normalization, and include the
        # characters that used to break the parse.
        wanted = re.search(r'Produce exactly these sections, in this order: (.+)', system)
        names = [n.strip() for n in (wanted.group(1) if wanted else 'Overview').split(',')]
        body = '# Mock drafted specification\n'
        for n in names:
            body += (f'\n## {n}\n\n(mock) {n} body grounded on the workspace. '
                     'It quotes "Booking date" and escapes a pipe \\| in a table.\n')
        return body
    if 'NOTE: <one line on what changed>' in system:
        return 'NOTE: rewrote it\n(mock) rewritten section body.'
    return None


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
        if 'remediation planner' in system:
            # a driver with two requirements citing it — the set case
            reply = json.dumps({
                'rationale': '(mock) the amendment is a change realized by two requirements.',
                'documents': [
                    {'kind': 'change', 'title': 'RTS 22 microsecond timestamps',
                     'path': 'changes/2026-08-rts22-precision.md',
                     'purpose': 'Records the amendment and why the specs must follow.'},
                    {'kind': 'requirement', 'title': 'Microsecond execution timestamps',
                     'path': 'requirements/REQ-exec-precision.md',
                     'purpose': 'States the precision the system must capture.', 'linksTo': [0]},
                    {'kind': 'requirement', 'title': 'Timestamp validation on ingest',
                     'path': 'requirements/REQ-timestamp-validation.md',
                     'purpose': 'States what happens when precision is missing.', 'linksTo': [0]},
                ],
            })
        elif 'regulatory deadline reader' in system:
            # a PROJECT recipe's stage (repo/.specquill/alignment/deadline-audit.md):
            # the mock answers a project's own pipeline exactly like a built-in,
            # because the engine does not distinguish them
            reply = json.dumps({'deadlines': [
                {'name': '(mock) T+1 transaction reporting',
                 'statement': 'Executed transactions must be reported by close of the '
                              'following working day.',
                 'clock': 'close of the following working day',
                 'paths': ['regulations/mifid-ii.md']},
            ]})
        elif 'requirements auditor' in system:
            reply = json.dumps({'findings': [{
                'anchor': 'regulations/mifid-ii.md#T+1',
                'kind': 'unstated-deadline',
                'severity': 'high',
                'title': '(mock) T+1 reporting deadline has no requirement',
                'detail': 'The regulation bounds submission to the following working day; '
                          'no requirement states the deadline.',
                'suggestedPath': 'requirements/REQ-submission-deadline.md',
                'evidence': [{'path': 'regulations/mifid-ii.md',
                              'quote': 'no later than the close of the following working day'}],
            }]})
        elif 'focus adviser' in system:
            # propose where a gap sweep would pay off
            reply = json.dumps({'areas': [
                {'name': 'Data retention',
                 'reason': '(mock) the retention rules have no requirement document.',
                 'sources': ['regulations']},
                {'name': 'Incident reporting',
                 'reason': '(mock) DORA windows are only partially covered.',
                 'sources': ['regulations']},
            ]})
        elif 'application surveyor' in system:
            # divide: two capability areas of the demo regulations source
            reply = json.dumps({'areas': [
                {'name': 'Transaction reporting',
                 'summary': '(mock) Submitting executed trades to the competent authority.',
                 'paths': ['regulations/mifid-ii.md']},
                {'name': 'Data protection',
                 'summary': '(mock) Retention limits on reported personal data.',
                 'paths': ['regulations/gdpr.md']},
            ]})
        elif 'requirements extractor' in system:
            # conquer: this area's requirements, evidence quoted VERBATIM
            if 'Data protection' in user:
                reply = json.dumps({'requirements': [{
                    'title': 'Storage limitation',
                    'statement': 'Report data SHALL be kept no longer than necessary for the '
                                 'reporting purpose.',
                    'evidence': [{'path': 'regulations/gdpr.md',
                                  'quote': 'kept for no longer than is necessary'}],
                }]})
            else:
                reply = json.dumps({'requirements': [
                    {
                        'title': 'Reporting deadline',
                        'statement': 'Executed transactions SHALL be reported no later than the '
                                     'close of the following working day.',
                        'evidence': [{'path': 'regulations/mifid-ii.md',
                                      'quote': 'no later than the close of the following working day'}],
                    },
                    {
                        'title': 'Timestamp precision',
                        'statement': 'Execution timestamps SHALL be captured to microsecond precision.',
                        'evidence': [{'path': 'regulations/mifid-ii.md',
                                      'quote': 'reported to **microsecond** precision'}],
                    },
                    {
                        'title': 'Hallucinated rule',
                        'statement': 'This one SHALL be dropped by evidence verification.',
                        'evidence': [{'path': 'regulations/mifid-ii.md', 'quote': 'NOT IN THE SOURCE'}],
                    },
                ]})
        elif 'coverage matcher' in system:
            # the walk: match each extracted requirement against the specs
            out = []
            for line in user.splitlines():
                m = re.match(r'^(\d+)\. \[', line)
                if not m:
                    continue
                i = int(m.group(1))
                if 'no later than' in line:
                    out.append({'index': i, 'coverage': 'full',
                                'document': 'requirements/REQ-042.md',
                                'note': '(mock) REQ-042 states the same deadline.'})
                elif 'microsecond' in line:
                    out.append({'index': i, 'coverage': 'partial',
                                'document': 'specs/txn-report.md',
                                'note': '(mock) the spec names the field but not the precision.'})
                else:
                    out.append({'index': i, 'coverage': 'none', 'document': '', 'note': ''})
            reply = json.dumps({'matches': out})
        elif 'remediation author' in system:
            # remedy/create: draft the document of the family being asked for.
            # The server enforces the folder, the family's `type:` and the
            # typed links; the mock only supplies plausible content.
            if 'work item document' in system:
                reply = json.dumps({
                    'path': 'work-items/WI-timestamp-precision.md',
                    'content': ('---\ntitle: Raise execution-timestamp precision\n'
                                'type: Work Item\nstatus: backlog\n---\n\n'
                                '# Raise execution-timestamp precision\n\n'
                                '(mock) Emit microsecond execution timestamps end to end.\n\n'
                                '- [ ] update the OMS transform\n- [ ] re-validate the mapping\n'),
                })
            elif 'requirement document' in system:
                # plain string split, not a regex: `([^.]+)\.` backtracks
                # polynomially on a prompt full of "Write this document: "
                # without a period (CodeQL py/polynomial-redos)
                _, found, rest = system.partition('Write this document: ')
                # first sentence, and never past the line: without a period a
                # naive split would swallow the rest of the prompt as a title
                title = rest.split('.', 1)[0].splitlines()[0].strip() if found else ''
                title = title or 'Extracted requirement'
                reply = json.dumps({
                    'path': 'requirements/REQ-mock.md',
                    'content': ('---\ntitle: ' + title + '\ntype: Requirement\nstatus: draft\n'
                                'priority: should\n---\n\n# ' + title + '\n\n'
                                '> (mock) The system SHALL satisfy "' + title + '".\n'),
                })
            else:
                reply = json.dumps({
                    'path': 'changes/2026-08-timestamp-precision.md',
                    'content': ('---\ntitle: RTS 22 microsecond timestamps\n'
                                'type: Change Record\nstatus: triage\nsource: regulatory\n---\n\n'
                                '# RTS 22 microsecond timestamps\n\n'
                                '(mock) The amendment tightens execution-timestamp precision to '
                                'microseconds; the affected documents must follow.\n'),
                })
        elif 'specquill linker' in system:
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
        elif 'source-drift auditor' in system and last['role'] != 'tool':
            # first round: consult the source, so the run's activity feed shows
            # real tool use (the drift engine narrates every call)
            tool_call = {'name': 'read_file',
                         'arguments': json.dumps({'path': '~regulations/regulations/mifid-ii.md'})}
            reply = ''
        elif 'source-drift auditor' in system:
            # second round: the findings. Evidence quotes the demo regulations
            # source VERBATIM (the server drops unverified quotes)
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
            }, {
                'anchor': (anchor.group(1) if anchor else 'REQ-042') + '#amendments',
                'source': 'regulations',
                'kind': 'new-requirement',
                'severity': 'medium',
                'title': '(mock) amendment tracking has no requirement',
                'detail': 'The regulation records dated amendments that must be tracked, '
                          'but no requirement states how amendments are picked up.',
                'suggestedPath': 'requirements/REQ-amendment-tracking.md',
                'sourcePaths': ['regulations/mifid-ii.md'],
                'evidence': [{'path': 'regulations/mifid-ii.md',
                              'quote': 'Execution timestamps must be captured'}],
            }]})
        elif (wiz := wizard_reply(system, body['messages'])) is not None:
            reply = wiz
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
            elif name in ('history', 'timeline'):
                reply = f"(mock) tool {name} returned {len(last['content'])} chars:\n{last['content'][:400]}"
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
        elif body.get('tools') and re.search(r'\bHISTORY\b', user):
            m = re.search(r'HISTORY (\S+)', user)
            arg = m.group(1) if m else ''
            args = {} if not arg else ({'sha': arg} if re.fullmatch(r'[0-9a-f]{7,40}', arg) else {'path': arg})
            tool_call = {'name': 'history', 'arguments': json.dumps(args)}
            reply = ''
        elif body.get('tools') and re.search(r'\bTIMELINE\b', user):
            m = re.search(r'TIMELINE (\S+)', user)
            args = {'state': m.group(1)} if m else {}
            tool_call = {'name': 'timeline', 'arguments': json.dumps(args)}
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
