#!/usr/bin/env python3
"""Static file server for the embed demo (docs/EMBEDDING.md).

Serves the built webui dist (terminal.html, editor.html, assets, wasm) and
this directory's demo page from ONE origin, so the demo works with zero
configuration. Alternative to `npx serve`:

    cd webui && npm run build && cd ..
    ./examples/embed/serve.py --port 8788

Then open http://localhost:8788/embed-demo.html
"""
import argparse
import functools
import os
import sys
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def main() -> int:
    parser = argparse.ArgumentParser(description="Serve the embed demo + webui dist on one origin")
    parser.add_argument("--port", type=int, default=8788)
    parser.add_argument(
        "--dist",
        default=os.path.join(ROOT, "webui", "dist"),
        help="webui dist directory (default: webui/dist)",
    )
    args = parser.parse_args()

    dist = os.path.abspath(args.dist)
    demo = os.path.join(os.path.dirname(os.path.abspath(__file__)))
    if not os.path.isfile(os.path.join(dist, "index.html")):
        print(f"error: {dist} does not look like a webui build (no index.html)", file=sys.stderr)
        print("build it first:  cd webui && npm run build", file=sys.stderr)
        return 1

    class Handler(SimpleHTTPRequestHandler):
        def translate_path(self, path: str) -> str:
            # The demo page (and a friendly root redirect); everything else
            # resolves inside the dist directory.
            if path == "/" or path.startswith("/embed-demo"):
                return os.path.join(demo, "embed-demo.html" if path.startswith("/embed-demo") else "index.html")
            return super().translate_path(path)

        # dist is a build output; the demo is static. Caching hurts iteration.
        def end_headers(self) -> None:
            self.send_header("Cache-Control", "no-cache")
            super().end_headers()

    handler = functools.partial(Handler, directory=dist)
    server = ThreadingHTTPServer(("127.0.0.1", args.port), handler)
    print(f"serving dist: {dist}")
    print(f"open: http://127.0.0.1:{args.port}/embed-demo.html")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
