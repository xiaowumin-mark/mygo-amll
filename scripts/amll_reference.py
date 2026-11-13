#!/usr/bin/env python3
"""Build and serve the pinned, unmodified AMLL renderer for local comparisons.

The optional Node toolchain and generated bundle stay outside this repository.
Only four loopback HTTP routes are exposed; the input TTML is never copied into
the repository or a public output directory. This harness is not the Native app.
"""

from __future__ import annotations

import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import subprocess
import tempfile
from urllib.parse import urlsplit


ROOT = Path(__file__).resolve().parents[1]
REFERENCE_COMMIT = "f32285864056587c4afa59d310208b0aa829cb53"
INPUT_LIMIT = 8 << 20


def build(args: argparse.Namespace, output: Path) -> dict[str, tuple[bytes, str]]:
    reference = args.reference.resolve()
    commit = subprocess.check_output(
        ["git", "-C", str(reference), "rev-parse", "HEAD"], text=True
    ).strip()
    if commit != REFERENCE_COMMIT:
        raise ValueError(f"reference must be at {REFERENCE_COMMIT}, got {commit}")
    status = subprocess.check_output(
        ["git", "-C", str(reference), "status", "--porcelain", "--untracked-files=no"],
        text=True,
    )
    if status:
        raise ValueError("reference has tracked edits; restore it before comparison")
    esbuild = args.node_modules.resolve() / "esbuild" / "lib" / "main.js"
    if not esbuild.is_file():
        raise ValueError("--node-modules must contain an externally installed esbuild")
    with args.ttml.open("rb") as file:
        lyrics = file.read(INPUT_LIMIT + 1)
    if len(lyrics) > INPUT_LIMIT:
        raise ValueError("local TTML exceeds the 8 MiB host input budget")
    output.mkdir(parents=True, exist_ok=True)
    config = {
        "commit": commit,
        "position": args.at,
        "font": args.font,
        "fontSize": args.font_size,
        "width": args.width,
        "height": args.height,
    }
    entry = (ROOT / "scripts" / "amll_reference.ts").read_text(encoding="utf-8")
    entry = entry.replace(
        '"REFERENCE_DOM"',
        json.dumps((reference / "packages/core/src/lyric-player/dom/index.ts").as_posix()),
    ).replace(
        '"REFERENCE_TTML"',
        json.dumps((reference / "packages/ttml/src/index.ts").as_posix()),
    ).replace('"REFERENCE_CONFIG"', json.dumps(config, ensure_ascii=True))
    (output / "entry.ts").write_text(entry, encoding="utf-8", newline="\n")
    builder = (
        f"import * as esbuild from {json.dumps(esbuild.as_uri())};\n"
        "await esbuild.build("
        + json.dumps(
            {
                "entryPoints": [str(output / "entry.ts")],
                "bundle": True,
                "format": "esm",
                "platform": "browser",
                "target": "chrome130",
                "outdir": str(output / "public"),
                "nodePaths": [str(args.node_modules.resolve())],
                "define": {"import.meta.env.DEV": "false"},
                "logLevel": "warning",
            }
        )
        + ");\n"
    )
    (output / "build.mjs").write_text(builder, encoding="utf-8", newline="\n")
    subprocess.run([args.node, str(output / "build.mjs")], check=True)
    html = b"""<!doctype html><html lang="en"><head><meta charset="utf-8">
<title>AMLL reference f322858</title><link rel="stylesheet" href="/entry.css">
<style>html,body{margin:0;background:rgb(18,21,31);overflow:hidden}
#stage{font-weight:600}#neutral{position:fixed;right:0;bottom:0;width:1px;height:1px;
padding:0;border:0;opacity:0;overflow:hidden;z-index:9999}</style></head><body>
<main id="stage"></main><button id="neutral">Capture neutral area</button>
<script type="module" src="/entry.js"></script></body></html>"""
    return {
        "/": (html, "text/html; charset=utf-8"),
        "/entry.js": ((output / "public/entry.js").read_bytes(), "text/javascript"),
        "/entry.css": ((output / "public/entry.css").read_bytes(), "text/css"),
        "/lyrics.ttml": (lyrics, "application/ttml+xml; charset=utf-8"),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ttml", type=Path, required=True)
    parser.add_argument("--node-modules", type=Path, required=True)
    parser.add_argument("--reference", type=Path, default=ROOT / "ref/applemusic-like-lyrics")
    parser.add_argument("--node", default="node")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--at", type=float, default=90000, help="initial media milliseconds")
    parser.add_argument("--width", type=int, default=960)
    parser.add_argument("--height", type=int, default=740)
    parser.add_argument("--font", default="Segoe UI, sans-serif")
    parser.add_argument("--font-size", type=float, default=30)
    parser.add_argument("--port", type=int, default=43821)
    args = parser.parse_args()
    import math

    if (
        not math.isfinite(args.at)
        or args.at < 0
        or not math.isfinite(args.font_size)
        or not 0 < args.font_size <= 512
        or not 1 <= args.width <= 4096
        or not 1 <= args.height <= 4096
        or not 1 <= args.port <= 65535
    ):
        parser.error("require finite nonnegative time and bounded font, viewport and port")
    with tempfile.TemporaryDirectory(prefix="mygo-amll-reference-") as temporary:
        output = (args.output or Path(temporary)).resolve()
        # Generated bundles must not enter the worktree or its ignored reference.
        if output == ROOT or ROOT in output.parents:
            parser.error("--output must be outside the repository")
        routes = build(args, output)

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                route = routes.get(urlsplit(self.path).path)
                if route is None:
                    self.send_error(404)
                    return
                data, content_type = route
                self.send_response(200)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(data)))
                self.send_header("Cache-Control", "no-store")
                self.send_header("X-Content-Type-Options", "nosniff")
                self.end_headers()
                self.wfile.write(data)

            def log_message(self, format: str, *values: object) -> None:
                pass

        with ThreadingHTTPServer(("127.0.0.1", args.port), Handler) as server:
            print(f"Unmodified AMLL {REFERENCE_COMMIT}", flush=True)
            print(f"Local comparison: http://127.0.0.1:{args.port}/", flush=True)
            print(f"Temporary bundle: {output}", flush=True)
            try:
                server.serve_forever()
            except KeyboardInterrupt:
                pass


if __name__ == "__main__":
    main()
