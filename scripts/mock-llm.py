#!/usr/bin/env python3
"""Tiny OpenAI-compatible /v1/chat/completions mock for dev & e2e.

Streams a canned grounded answer for chat; returns a canned edit-JSON for the
copilot draft prompt (fixes the RTS 22 timestamp drift in the demo repo).
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

        if 'Reply with ONLY a JSON object' in system:
            reply = DRAFT_REPLY
        else:
            n_files = len(re.findall(r'^## \S+\.(?:md|ya?ml|json|mermaid)$', system, re.M))
            focus = re.search(r'currently viewing: (\S+)', system)
            reply = (f"(mock) I am grounded on {n_files} workspace files"
                     + (f", focused on {focus.group(1)}" if focus else '')
                     + f". You asked: “{user[:120]}” — in the demo workspace the "
                       "RTS 22 amendment drives REQ-042 and the drifted mapping is "
                       "trade.executionTimestamp (see data-mappings/trade.md).")

        if body.get('stream'):
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Transfer-Encoding', 'chunked')
            self.end_headers()
            for i in range(0, len(reply), 24):
                chunk = json.dumps({'choices': [{'delta': {'content': reply[i:i + 24]}}]})
                self._chunk(f"data: {chunk}\n\n")
                time.sleep(0.01)
            self._chunk("data: [DONE]\n\n")
            self._chunk('')
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
