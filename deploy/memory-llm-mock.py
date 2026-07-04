#!/usr/bin/env python3
import hashlib
import json
from http.server import BaseHTTPRequestHandler, HTTPServer


def _embedding_from_text(text: str):
    data = hashlib.sha256(text.encode("utf-8")).digest()
    vector = []
    digest_len = len(data)
    for idx in range(1024):
        start = (idx * 4) % digest_len
        chunk = data[start:start + 4]
        if len(chunk) < 4:
            chunk = chunk.ljust(4, b"\x00")
        vector.append(int.from_bytes(chunk, "big") / 4294967295.0)
    return vector


class Handler(BaseHTTPRequestHandler):
    def _read_json(self):
        length = int(self.headers.get("content-length", "0"))
        raw = self.rfile.read(length)
        if not raw:
            return {}
        return json.loads(raw.decode("utf-8"))

    def _write_json(self, status: int, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        if self.path == "/v1/embeddings":
            payload = self._read_json()
            inputs = payload.get("input", [])
            if isinstance(inputs, str):
                inputs = [inputs]
            vectors = [_embedding_from_text(str(text)) for text in inputs]
            data = [{"object": "embedding", "index": idx, "embedding": vector} for idx, vector in enumerate(vectors)]
            self._write_json(200, {"object": "list", "data": data})
            return
        if self.path == "/v1/rerank":
            payload = self._read_json()
            documents = payload.get("documents", [])
            query = str(payload.get("query", ""))
            query_tokens = set(query.lower().split())
            results = []
            for idx, doc in enumerate(documents):
                doc_tokens = set(str(doc).lower().split())
                if query_tokens:
                    overlap = len(query_tokens & doc_tokens)
                    score = overlap / float(max(len(query_tokens), 1))
                else:
                    score = 0.0
                results.append({"index": idx, "relevance_score": score})
            self._write_json(200, {"results": results})
            return
        if self.path == "/healthz":
            self._write_json(200, {"status": "ok"})
            return
        self.send_response(404)
        self.end_headers()
        self.wfile.write(b"{}");


if __name__ == "__main__":
    server = HTTPServer(("0.0.0.0", 11434), Handler)
    server.serve_forever()
