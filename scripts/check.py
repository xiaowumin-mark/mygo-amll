#!/usr/bin/env python3
"""Run the same portable core checks as CI; native rendering has separate gates."""

from __future__ import annotations

import argparse
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]


def run(*args: str, capture: bool = False) -> str:
    print("+ " + " ".join(args), flush=True)
    result = subprocess.run(
        args,
        cwd=ROOT,
        check=True,
        text=True,
        encoding="utf-8",
        stdout=subprocess.PIPE if capture else None,
    )
    return result.stdout or ""


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--no-race",
        action="store_true",
        help="skip the race detector only when the local C toolchain is unavailable",
    )
    args = parser.parse_args()

    # Include build-tagged/platform-specific files, and exclude ignored ref clones.
    output = run(
        "git",
        "ls-files",
        "-z",
        "--cached",
        "--others",
        "--exclude-standard",
        "--",
        "*.go",
        capture=True,
    )
    go_files = [
        str(ROOT / name)
        for name in output.split("\0")
        if name and (ROOT / name).is_file()
    ]
    if not go_files:
        raise RuntimeError("no Go source files found")
    unformatted = run("gofmt", "-l", *sorted(set(go_files)), capture=True).strip()
    if unformatted:
        print("Run gofmt on:\n" + unformatted, file=sys.stderr)
        return 1

    run("go", "vet", "./...")
    command = ["go", "test", "-count=1"]
    if not args.no_race:
        if run("go", "env", "CGO_ENABLED", capture=True).strip() != "1":
            print(
                "Race checks need CGO_ENABLED=1 and a C compiler. "
                "Use --no-race only for local checks; CI still requires race checks.",
                file=sys.stderr,
            )
            return 1
        command.append("-race")
    run(*command, "./...")
    run(sys.executable, "-m", "unittest", "discover", "-s", "scripts", "-p", "test_*.py")
    run("git", "diff", "--check")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (subprocess.CalledProcessError, FileNotFoundError, RuntimeError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1) from error
