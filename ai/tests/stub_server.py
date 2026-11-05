"""Minimal OpenAI-compatible stub for testing the inference contract.

Env: STUB_MODELS (comma list), STUB_DIMS (embedding size),
     STUB_NO_TOOLS=1 (never return tool_calls), STUB_PORT.
"""
import hashlib, json, os
from http.server import BaseHTTPRequestHandler, HTTPServer

MODELS = os.environ.get("STUB_MODELS", "").split(",")
DIMS = int(os.environ.get("STUB_DIMS", "768"))
NO_TOOLS = os.environ.get("STUB_NO_TOOLS") == "1"

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, obj):
        b = json.dumps(obj).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        if self.path == "/api/version": return self.send(200, {"version": "stub"})
        if self.path == "/v1/models":
            return self.send(200, {"object": "list", "data": [{"id": m, "object": "model"} for m in MODELS]})
        self.send(404, {"error": "not found"})
    def do_POST(self):
        req = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if req.get("model") not in MODELS: return self.send(404, {"error": "model not found"})
        if self.path == "/v1/chat/completions":
            msg = {"role": "assistant", "content": "ready"}
            if req.get("tools") and not NO_TOOLS:
                fn = req["tools"][0]["function"]["name"]
                msg = {"role": "assistant", "content": "", "tool_calls": [{"id": "c1", "type": "function",
                       "function": {"name": fn, "arguments": json.dumps({"city": "Paris"})}}]}
            return self.send(200, {"object": "chat.completion", "choices": [{"index": 0, "message": msg}]})
        if self.path == "/v1/embeddings":
            h = hashlib.sha256(req["input"].encode()).digest()
            vec = [((h[i % 32] / 255.0) - 0.5) for i in range(DIMS)]
            return self.send(200, {"object": "list", "data": [{"index": 0, "embedding": vec}]})
        self.send(404, {"error": "not found"})

HTTPServer(("127.0.0.1", int(os.environ.get("STUB_PORT", "18080"))), H).serve_forever()
