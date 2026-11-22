import json
from pathlib import Path
import sys

# The sandbox starts Python in isolated mode. Only import sibling oracle modules.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from collaboration import collaboration_checks
from delivery_checks import delivery_checks
from product_checks import product_checks


def main():
    workspace, output = map(Path, sys.argv[1:3])
    trace = Path(sys.argv[3]) if len(sys.argv) > 3 else None
    contract = workspace / "CONTRACT.md"
    original = Path(__file__).parent / "fixture" / contract.name
    intact = contract.is_file() and not contract.is_symlink() and contract.read_bytes() == original.read_bytes()
    checks = [{"id": "contract", "status": "passed" if intact else "failed",
               "evidence": "Compared the delivered contract with the frozen fixture."}]
    metadata = output / "input.json"
    change_ns = json.loads(metadata.read_text()).get("workspace_change_ns") if metadata.is_file() else None
    checks.extend(collaboration_checks(trace, change_ns))
    checks.extend(product_checks(workspace))
    checks.extend(delivery_checks(workspace))
    (output / "result.json").write_text(json.dumps({"schema_version": 1, "checks": checks}, ensure_ascii=False))


if __name__ == "__main__":
    main()
