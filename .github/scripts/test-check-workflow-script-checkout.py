#!/usr/bin/env python3
import importlib.util
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-workflow-script-checkout.py")
spec = importlib.util.spec_from_file_location("checkout_check", SCRIPT)
assert spec and spec.loader
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


class WorkflowCheckoutTest(unittest.TestCase):
    def run_checker(self, workflow: str, scripts: tuple[str, ...] = ()) -> tuple[int, str]:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            workflow_dir = root / "workflows"
            workflow_dir.mkdir()
            for script in scripts:
                path = root / script
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("", encoding="utf-8")
            (workflow_dir / "test.yml").write_text(workflow, encoding="utf-8")
            old_workflow_dir = checker.WORKFLOW_DIR
            checker.WORKFLOW_DIR = workflow_dir
            old_cwd = Path.cwd()
            try:
                import os

                os.chdir(root)
                from contextlib import redirect_stderr, redirect_stdout
                from io import StringIO

                output = StringIO()
                with redirect_stdout(output), redirect_stderr(output):
                    result = checker.main()
                return result, output.getvalue()
            finally:
                os.chdir(old_cwd)
                checker.WORKFLOW_DIR = old_workflow_dir

    def test_missing_checkout_is_rejected_for_python_and_node(self):
        result, output = self.run_checker(
            """jobs:\n  test:\n    steps:\n      - run: python3 .github/scripts/check.py\n      - run: node deploy/check.mjs\n""",
            (".github/scripts/check.py", "deploy/check.mjs"),
        )
        self.assertEqual(result, 1)
        self.assertIn("without actions/checkout", output)

    def test_checkout_must_precede_script(self):
        result, output = self.run_checker(
            """jobs:\n  test:\n    steps:\n      - run: python3 .github/scripts/check.py\n      - uses: actions/checkout@v7\n""",
            (".github/scripts/check.py",),
        )
        self.assertEqual(result, 1)
        self.assertIn("must precede", output)

    def test_checkout_before_shell_python_and_node_scripts_passes(self):
        result, output = self.run_checker(
            """jobs:\n  test:\n    steps:\n      - uses: actions/checkout@v7\n      - run: bash deploy/check.sh\n      - run: python3 .github/scripts/check.py\n      - run: node deploy/check.js\n""",
            ("deploy/check.sh", ".github/scripts/check.py", "deploy/check.js"),
        )
        self.assertEqual(result, 0)
        self.assertIn("PASS", output)


if __name__ == "__main__":
    unittest.main()
