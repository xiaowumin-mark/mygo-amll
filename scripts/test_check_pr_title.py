import os
from pathlib import Path
import subprocess
import sys
import unittest

from check_pr_title import valid_title


class TitleTests(unittest.TestCase):
    def test_release_relevant_titles(self):
        for title in (
            "feat(timeline): 支持定位",
            "fix: handle empty lyrics",
            "feat!: change public API",
            "perf(native/mygo): reuse buffers",
            "style: format code",
            "fix(native.mygo_v2): release resources",
            "chore(master): release 0.1.0",
        ):
            with self.subTest(title=title):
                self.assertTrue(valid_title(title))

    def test_missing_or_ambiguous_structure(self):
        for title in (
            "",
            "add a feature",
            "feature: add timing",
            "Feat: add timing",
            "fix:",
            "fix:  ",
            "fix: text\nfeat: another",
            "fix: text\rfeat: another",
            " fix: text",
            "fix: text ",
            "fix(Timeline): text",
            "fix(.timeline): text",
            "fix(_timeline): text",
            "fix: " + "x" * 96,
        ):
            with self.subTest(title=title):
                self.assertFalse(valid_title(title))

    def test_cli_argument_and_environment(self):
        script = str(Path(__file__).with_name("check_pr_title.py"))
        cases = (
            (["style(native.mygo_v2): format code"], "invalid", 0),
            ([], "feat(timeline): add seek", 0),
            (["invalid"], "feat: valid environment", 1),
            ([""], "feat: valid environment", 1),
            ([], "", 1),
        )
        for arguments, environment, expected in cases:
            with self.subTest(arguments=arguments, environment=environment):
                result = subprocess.run(
                    [sys.executable, "-B", script, *arguments],
                    env={**os.environ, "PR_TITLE": environment},
                    capture_output=True,
                    text=True,
                    check=False,
                )
                self.assertEqual(result.returncode, expected, result.stderr)


if __name__ == "__main__":
    unittest.main()
