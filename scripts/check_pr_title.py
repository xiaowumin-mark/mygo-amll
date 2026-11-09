#!/usr/bin/env python3
"""Validate a Conventional Commit title from an argument or PR_TITLE."""

import argparse
import os
import re
import sys


PATTERN = re.compile(
    r"(?:feat|fix|perf|docs|style|refactor|test|build|ci|chore|revert)"
    r"(?:\([a-z0-9][a-z0-9/._-]*\))?!?: [^\s].*"
)


def valid_title(title: str) -> bool:
    return (
        len(title) <= 100
        and title == title.strip()
        and "\n" not in title
        and "\r" not in title
        and PATTERN.fullmatch(title) is not None
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("title", nargs="?", help="defaults to the PR_TITLE environment variable")
    args = parser.parse_args()
    title = args.title if args.title is not None else os.environ.get("PR_TITLE", "")
    if not valid_title(title):
        print(
            "PR title must be <=100 characters and follow "
            "type(scope)!: description; e.g. feat(timeline): support seek",
            file=sys.stderr,
        )
        raise SystemExit(1)
