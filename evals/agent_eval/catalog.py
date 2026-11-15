from pathlib import Path
import re

from .repository import validate_repository
from .storage import read_json, tree_hash, digest, json_hash

ROOT = Path(__file__).resolve().parents[1]


def inside(root, relative):
    path = (root / relative).resolve()
    if not path.is_relative_to(root.resolve()):
        raise ValueError(f"path escapes case: {relative}")
    return path


def load_case(name, root=ROOT, case_dir=None):
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", name):
        raise ValueError("invalid case ID")
    directory = Path(case_dir) if case_dir else Path(root) / "cases" / name
    data = read_json(directory / "case.json")
    allowed = {"schema_version", "id", "version", "title", "tags", "prompt", "fixture", "verifier", "requirements", "budgets", "source", "kind"}
    if set(data) - {'repository'} != allowed or data["schema_version"] != 1 or data["id"] != name:
        raise ValueError(f"invalid case manifest: {name}")
    if type(data["version"]) is not int or data["version"] < 1:
        raise ValueError("case version must be a positive integer")
    for key in ["title", "kind", "source"]:
        if not isinstance(data[key], str) or not data[key].strip():
            raise ValueError(f"invalid {key}")
    if data["kind"] not in ["product", "review"]:
        raise ValueError("invalid case kind")
    if not isinstance(data["tags"], list) or not data["tags"] or not all(isinstance(t, str) for t in data["tags"]):
        raise ValueError("tags required")
    requirements = data["requirements"]
    if not isinstance(requirements, list) or not requirements:
        raise ValueError("requirements required")
    if any(not isinstance(r, dict) or set(r) != {"id", "description", "required"} or not isinstance(r["required"], bool) or not isinstance(r.get("description"), str) or not r["description"].strip() for r in requirements):
        raise ValueError("invalid requirement")
    ids = [r["id"] for r in requirements]
    if any(not isinstance(i, str) or not i for i in ids) or len(set(ids)) != len(ids):
        raise ValueError("requirement IDs must be unique")
    budgets = data["budgets"]
    if set(budgets) != {"wall_seconds", "model_calls", "total_tokens", "verifier_seconds"}:
        raise ValueError("invalid budgets")
    if any(type(v) is not int or v < 1 for v in budgets.values()):
        raise ValueError("budgets must be positive integers")
    paths = {key: inside(directory, data[key]) for key in ['prompt', 'verifier']}
    repository = data.get('repository')
    if repository is not None:
        validate_repository(repository)
        if data['fixture'] is not None or data['kind'] != 'review':
            raise ValueError('repository cases must be reviews with fixture=null')
        paths['fixture'] = None
    else:
        if not isinstance(data['fixture'], str):
            raise ValueError('fixture must name a local directory')
        paths['fixture'] = inside(directory, data['fixture'])
        if not paths['fixture'].is_dir():
            raise ValueError('case input missing')
        for p in paths['fixture'].rglob('*'):
            if p.is_symlink():
                raise ValueError('seed fixtures cannot contain symlinks')
    if not all(paths[k].is_file() for k in ['prompt', 'verifier']):
        raise ValueError('case input missing')
    receipt = {'manifest': json_hash(data), 'prompt': digest(paths['prompt'].read_bytes()),
               'fixture': tree_hash(paths['fixture']) if paths['fixture'] is not None else None,
               'verifier': digest(paths['verifier'].read_bytes()), 'all_case_resources': tree_hash(directory)}
    return {"manifest": data, "paths": paths, "directory": directory, "receipt": receipt,
            "fingerprint": json_hash(receipt)}


def load_suite(name, root=ROOT):
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", name):
        raise ValueError("invalid suite ID")
    data = read_json(Path(root) / "suites" / (name + ".json"))
    if set(data) != {"schema_version", "id", "version", "cases"} or data["schema_version"] != 1 or data["id"] != name:
        raise ValueError("invalid suite")
    if type(data["version"]) is not int or data["version"] < 1:
        raise ValueError("invalid suite version")
    if not data["cases"] or len(set(data["cases"])) != len(data["cases"]):
        raise ValueError("suite cases must be nonempty and unique")
    return data, [load_case(case, root) for case in data["cases"]]
