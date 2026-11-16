#!/usr/bin/env python3
"""Fail when a workflow runs a tracked repository script before checkout."""

from pathlib import Path
import re
import sys

WORKFLOW_DIR = Path(".github/workflows")
SCRIPT_REF = re.compile(
    r"(?<![A-Za-z0-9_.-])((?:\./)?(?:\.github|backend|deploy)/[A-Za-z0-9_./-]+\.(?:sh|py|mjs|js))(?![A-Za-z0-9_.-])"
)
JOB_HEADER = re.compile(r"^  ([A-Za-z0-9_-]+):\s*$")


def iter_jobs(text):
    in_jobs = False
    current = None
    lines = []
    for line in text.splitlines():
        if line == "jobs:":
            in_jobs = True
            continue
        if not in_jobs:
            continue
        match = JOB_HEADER.match(line)
        if match:
            if current is not None:
                yield current, lines
            current = match.group(1)
            lines = []
        elif current is not None:
            lines.append(line)
    if current is not None:
        yield current, lines


def main():
    errors = []
    workflows = sorted(WORKFLOW_DIR.glob("*.y*ml"))
    if not workflows:
        print("No GitHub Actions workflows found.")
        return 0

    for workflow in workflows:
        text = workflow.read_text(encoding="utf-8")
        for job, lines in iter_jobs(text):
            job_text = "\n".join(lines)
            refs = {match.group(1).removeprefix("./") for match in SCRIPT_REF.finditer(job_text)}
            if not refs:
                continue
            checkout_lines = [index for index, line in enumerate(lines) if re.search(r"uses:\s*actions/checkout@", line)]
            has_checkout = bool(checkout_lines)
            if not has_checkout:
                for ref in sorted(refs):
                    errors.append(f"{workflow}:{job}: {ref} is used without actions/checkout")
            else:
                first_checkout = min(checkout_lines)
                for ref in sorted(refs):
                    if any(index < first_checkout for index, line in enumerate(lines) if ref in line or f"./{ref}" in line):
                        errors.append(f"{workflow}:{job}: actions/checkout must precede {ref}")
            for ref in sorted(refs):
                if not Path(ref).is_file():
                    errors.append(f"{workflow}:{job}: referenced script does not exist: {ref}")

    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"PASS: checked {len(workflows)} workflow file(s) for repository-script checkout coverage")
    return 0


if __name__ == "__main__":
    sys.exit(main())
