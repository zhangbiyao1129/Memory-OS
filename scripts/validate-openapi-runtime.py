#!/usr/bin/env python3
import json
import sys
import urllib.request


def load_spec(source: str) -> dict:
    if source.startswith("http://") or source.startswith("https://"):
        with urllib.request.urlopen(source, timeout=10) as response:
            return json.load(response)
    with open(source, encoding="utf-8") as handle:
        return json.load(handle)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: validate-openapi-runtime.py <openapi-url-or-file>", file=sys.stderr)
        return 2
    spec = load_spec(sys.argv[1])
    if spec.get("openapi") != "3.0.3":
        print("unexpected openapi version", file=sys.stderr)
        return 1
    paths = spec.get("paths") or {}
    required = {
        "/healthz": "get",
        "/openapi.json": "get",
        "/version": "get",
        "/memory/turn-event": "post",
        "/memory/search": "post",
        "/memory/qdrant/status": "post",
    }
    for path, method in required.items():
        if path not in paths:
            print(f"missing openapi path: {path}", file=sys.stderr)
            return 1
        if method not in paths[path]:
            print(f"missing openapi method: {method} {path}", file=sys.stderr)
            return 1
    print(f"openapi ok: {len(paths)} paths")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
