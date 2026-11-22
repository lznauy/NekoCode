import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
from datetime import datetime, timezone
import uuid

# Bump when execution, delivery scoring or cost accounting semantics change.
# Source fingerprints remain provenance, including presentation-only edits.
EVALUATION_CONTRACT_VERSION = 5


def digest(value):
    return hashlib.sha256(value).hexdigest()


def json_hash(value):
    return digest(json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode())


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_name(path.name + ".tmp")
    temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    os.replace(temp, path)


def read_json(path):
    return json.loads(Path(path).read_text())


def new_id(prefix):
    return f"{prefix}-{datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')}-{uuid.uuid4().hex[:10]}"


def tree_hash(root):
    root = Path(root)
    files = {}
    for path in sorted(root.rglob("*")):
        if any(p in {".git", "__pycache__"} for p in path.relative_to(root).parts):
            continue
        if path.is_symlink():
            files[path.relative_to(root).as_posix()] = {"symlink": os.readlink(path)}
        elif path.is_file():
            files[path.relative_to(root).as_posix()] = {"sha256": digest(path.read_bytes()), "executable": bool(path.stat().st_mode & 0o111)}
    return json_hash(files)


def workspace_change_ns(root):
    """Sample the original Linux tree before copying changes its inode timestamps."""
    def latest(path):
        timestamp = path.lstat().st_ctime_ns
        if path.is_symlink() or not path.is_dir():
            return timestamp
        for entry in path.iterdir():
            if entry.name not in (".git", "__pycache__") and not entry.name.endswith(".pyc"):
                timestamp = max(timestamp, latest(entry))
        return timestamp

    try:
        return latest(Path(root))
    except (OSError, RecursionError):
        return None


def copy_tree(source, target):
    # Preserve symlinks as links, never follow agent-created links into host data.
    shutil.copytree(source, target, symlinks=True, ignore=shutil.ignore_patterns(".git", "__pycache__", "*.pyc"))


def validate_output_directory(repo, output):
    """Keep generated evidence outside source, using Git's existing boundary."""
    repo, output = Path(repo).resolve(), Path(output).resolve()
    if not output.is_relative_to(repo):
        return
    relative = output.relative_to(repo).as_posix()
    tracked = subprocess.check_output(["git", "-C", str(repo), "ls-files", "-z", "--", ":(literal)" + relative])
    if tracked:
        raise ValueError("output directory contains tracked source files; choose an ignored directory or a path outside the repository")
    ignored = subprocess.run(["git", "-C", str(repo), "check-ignore", "-q", "--", relative + "/"])
    if ignored.returncode != 0:
        raise ValueError("output inside the source repository must be a Git-ignored directory; choose an external path or explicitly ignore the output root")


def source_receipt(repo):
    """Record source identity without archiving project files or credentials."""
    def git(*args):
        return subprocess.check_output(['git', '-C', str(repo), *args]).decode().strip()
    return {'commit': git('rev-parse', 'HEAD'),
            'dirty': bool(git('status', '--porcelain', '--untracked-files=normal')),
            'note': 'Source identity does not prove the supplied binary was built from this checkout.'}


def configuration_receipt(root):
    """Hash configuration inputs without copying any credentials into artifacts."""
    root = Path(root)
    hashes = {}
    for name in ["config.json", "permissions.json", "plugins.json", "settings.json"]:
        path = root / name
        if path.is_file():
            hashes[name] = digest(path.read_bytes())
    for name in ["skills", "agents", "hooks", "rules", "plugins"]:
        path = root / name
        if path.is_dir():
            hashes[name] = tree_hash(path)
    return {"sha256": json_hash(hashes), "input_hashes": hashes,
            "limitations": ["Environment credentials, remote MCP state and files outside this config tree are not pinned.",
                            "A runtime binary can discover additional project/ancestor settings; inspect observed tools/extensions."]}


def evaluator_fingerprint():
    directory = Path(__file__).resolve().parent
    return json_hash({p.name: digest(p.read_bytes()) for p in sorted(directory.glob("*.py"))})
