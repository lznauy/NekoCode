"""Inspect project deliverables, then run the candidate suite against a semantic control."""

import ast
import io
import json
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import tokenize


def regular(path, root):
    if not path.is_relative_to(root):
        raise ValueError(f"path outside the project: {path}")
    current = root
    for part in path.relative_to(root).parts:
        current = current / part
        if current.is_symlink():
            raise ValueError(f"unexpected symlink: {current.relative_to(root)}")
    if not path.is_file():
        raise ValueError(f"missing regular file: {path.relative_to(root)}")
    return path


def design(root):
    readme = regular(root / "README.md", root).read_text()
    for command in ("python3 -m relay", "python3 -m unittest discover -s tests"):
        if command not in readme:
            raise ValueError(f"README.md needs an example using {command}")
    doc = regular(root / "docs/design.md", root)
    text = doc.read_text()
    for concept in (r"sqlite", r"事务|transaction", r"重试|幂等|retr(?:y|ies)|idempoten", r"测试|验证|test|verif"):
        if not re.search(concept, text, re.IGNORECASE):
            raise ValueError(f"docs/design.md does not discuss {concept}")
    links = set()
    for target in re.findall(r"\[[^\]\n]*\]\(([^)\n]+)\)", text):
        target = target.partition("#")[0].strip("<>")
        if target and target.endswith(".py") and not re.match(r"\w+:", target):
            # Resolve .. only after checking the link components for symlinks.
            if Path(target).is_absolute():
                raise ValueError(f"expected a project-relative design link: {target}")
            current = doc.parent
            for part in Path(target).parts:
                current = current.parent if part == ".." else current / part
                if not current.is_relative_to(root) or current.is_symlink():
                    raise ValueError(f"invalid project link: {target}")
            links.add(regular(current, root).relative_to(root))
    modules = sorted(str(p) for p in links if p.parts[0] == "relay")
    tests = sorted(str(p) for p in links if p.parts[0] == "tests" and p.name.startswith("test_"))
    if len(modules) < 3 or len(tests) < 2:
        raise ValueError(f"design links resolved to {len(modules)} implementation and {len(tests)} test files")
    return f"README commands present; design links resolve to {', '.join(modules + tests)}"


def code_lines(path):
    source = path.read_bytes()
    tree = ast.parse(source, filename=str(path))
    documentation = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Expr) and isinstance(node.value, ast.Constant) and isinstance(node.value.value, str):
            documentation.update(range(node.lineno, node.end_lineno + 1))
    lines = set()
    for token in tokenize.tokenize(io.BytesIO(source).readline):
        if token.type in (tokenize.NAME, tokenize.NUMBER, tokenize.STRING) or (
            token.type == tokenize.OP and token.string not in "()[]{}.,:"
        ):
            lines.add(token.start[0])
    return len(lines - documentation)


def scale(root):
    counts = {}
    for group in ("relay", "tests"):
        folder = root / group
        if not folder.is_dir() or folder.is_symlink():
            raise ValueError(f"missing source directory: {group}")
        counts[group] = sum(code_lines(regular(path, root)) for path in folder.rglob("*.py")
                            if "__pycache__" not in path.parts)
    message = f"relay={counts['relay']}, tests={counts['tests']}, total={sum(counts.values())} Python code lines"
    if counts["relay"] < 500 or sum(counts.values()) < 1000:
        raise ValueError(message)
    return message


def copy_product(source, dest):
    def ignore(folder, names):
        skipped = {name for name in names if name in (".git", "__pycache__")}
        for name in set(names) - skipped:
            path = Path(folder) / name
            mode = path.lstat().st_mode
            if not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                raise ValueError(f"cannot stage non-regular entry: {path.relative_to(source)}")
        return skipped

    shutil.copytree(source, dest, ignore=ignore)


# Run the same discovered suite twice. Only assertion failures on the control
# count as detecting the change; an import error or a timeout is inconclusive.
TEST_RUNNER = """
import json, os, sys, unittest
sys.path.insert(0, os.getcwd())
suite = unittest.defaultTestLoader.discover('tests')
result = unittest.TestResult()
suite.run(result)
print('RELAY_TEST_RESULT ' + json.dumps(dict(
    run=result.testsRun, failures=len(result.failures), errors=len(result.errors),
    skipped=len(result.skipped), expected=len(result.expectedFailures),
    success=result.wasSuccessful(), sample=[t.id() for t, _ in result.failures[:3]])))
"""


def run_suite(project):
    with tempfile.TemporaryFile() as log:
        result = subprocess.run([sys.executable, "-I", "-B", "-c", TEST_RUNNER], cwd=project,
                                stdin=subprocess.DEVNULL, stdout=log, stderr=log, timeout=45)
        log.seek(max(0, log.tell() - 8192))
        tail = log.read().decode(errors="replace")
    last = tail.strip().splitlines()
    if result.returncode or not last or not last[-1].startswith("RELAY_TEST_RESULT "):
        raise ValueError(f"submitted suite did not finish: exit {result.returncode}: {tail[-240:]}")
    return json.loads(last[-1].removeprefix("RELAY_TEST_RESULT "))


# Other commands and error paths run the submitted entry point unchanged.
# Successful route replies retain their schema and path but misreport cost.
ROUTE_CONTROL = """
import json, os, pathlib, subprocess, sys
if sys.argv[-1:] == ['route'] and not os.environ.get('RELAY_EVAL_ORIGINAL_ENTRY'):
    child = subprocess.run([sys.executable, '-m', 'relay', *sys.argv[1:]],
                           env=dict(os.environ, RELAY_EVAL_ORIGINAL_ENTRY='1'), stdout=subprocess.PIPE)
    data = child.stdout
    if child.returncode == 0:
        reply = json.loads(data)
        reply['minutes'] += 1
        data = (json.dumps(reply) + '\\n').encode()
    sys.stdout.buffer.write(data)
    raise SystemExit(child.returncode)
exec(compile(pathlib.Path(__file__).with_name('.relay-original-entry').read_bytes(), __file__, 'exec'))
"""


def project_tests(root):
    regular(root / "relay/__main__.py", root)
    with tempfile.TemporaryDirectory() as temp:
        baseline = Path(temp) / "baseline"
        copy_product(root, baseline)
        healthy = run_suite(baseline)
        passed = healthy["run"] - healthy["skipped"] - healthy["expected"]
        if not healthy["success"] or passed < 12:
            raise ValueError(f"submitted suite needs 12 passing tests: {healthy}")
        control = Path(temp) / "control"
        copy_product(root, control)
        entry = control / "relay/__main__.py"
        original = entry.with_name(".relay-original-entry")
        if original.exists():
            raise ValueError("control entry name collides with submission")
        entry.rename(original)
        entry.write_text(ROUTE_CONTROL)
        broken = run_suite(control)
        if broken["run"] != healthy["run"] or not broken["failures"]:
            raise ValueError(f"route time mutation needs a failing assertion in the same suite: {broken}")
        return f"{passed} passing tests; wrong route minutes detected by {', '.join(broken['sample'])}"


def delivery_checks(product):
    root = Path(product).resolve()
    results = []
    for key, probe in (("design", design), ("scale", scale), ("project-tests", project_tests)):
        try:
            evidence, status = probe(root), "passed"
        except (OSError, ValueError, SyntaxError, tokenize.TokenError, subprocess.SubprocessError) as exc:
            evidence, status = str(exc), "failed"
        results.append(dict(id=key, status=status, evidence=evidence[:800]))
    return results
