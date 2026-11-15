import argparse
import json
import os
from pathlib import Path
import sys
import tempfile

from .catalog import ROOT, load_case, load_suite
from .compare import compare
from .runner import run_trial
from .storage import configuration_receipt, new_id, read_json, source_receipt, write_json, validate_output_directory
from .verification import verify
from .layout import TrialLayout, new_verification
from .history import collect, refresh, markdown


def main():
    parser = argparse.ArgumentParser(description="NekoCode versioned task evaluation")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("list")
    history = commands.add_parser("history", help="Browse old and new run metadata without loading traces")
    history.add_argument("--root", type=Path, default=ROOT / "runs")
    history.add_argument("--case")
    history.add_argument("--limit", type=int, default=20)
    history.add_argument("--refresh", action="store_true", help="Regenerate index.json and index.md")
    validate = commands.add_parser("validate")
    validate.add_argument("--suite", default="smoke")
    run = commands.add_parser("run")
    run.add_argument("--suite", default="smoke")
    run.add_argument("--label", help="Human-readable label; immutable run IDs still identify results")
    run.add_argument("--case", action="append", dest="cases")
    run.add_argument("--binary", required=True, type=Path)
    run.add_argument("--model", required=True)
    run.add_argument("--mode", choices=["standard", "super-agent"], default="standard")
    run.add_argument("--repeat", type=int, default=1, choices=range(1, 6))
    run.add_argument("--output", type=Path, default=ROOT / "runs")
    run.add_argument("--telemetry-root", type=Path, default=Path.home() / ".nekocode")
    run.add_argument("--interaction-policy", choices=["deny", "allow"], default="deny")
    run.add_argument("--config-fingerprint", help="SHA256 of the explicit effective external configuration; no credentials")
    run.add_argument("--allow-user-config", action="store_true", help="Acknowledge real configured model calls, local code execution and configured plugins/hooks")
    comp = commands.add_parser("compare")
    comp.add_argument("baseline", type=Path)
    comp.add_argument("candidate", type=Path)
    comp.add_argument("--output", type=Path, help="Default: evals/comparisons/<unique-id>")
    recheck = commands.add_parser("reverify", help="Recheck a saved product using its frozen case; never edits the original result")
    recheck.add_argument("trial", type=Path)
    recheck.add_argument("--output", type=Path, help="Default: fresh trial/verification/vN")
    check = commands.add_parser("verify")
    check.add_argument("case")
    check.add_argument("workspace", type=Path)
    check.add_argument("--output", type=Path, required=True)
    check.add_argument("--answer", type=Path)
    args = parser.parse_args()
    if getattr(args, "output", None) is not None:
        validate_output_directory(ROOT.parent, args.output)
    if args.command == "history":
        if args.refresh:
            validate_output_directory(ROOT.parent, args.root)
        if args.limit < 1:
            parser.error("--limit must be positive")
        data = refresh(args.root) if args.refresh else collect(args.root)
        if args.case:
            data["records"] = [r for r in data["records"] if r["case_id"] == args.case]
        data["records"] = data["records"][:args.limit]
        print(markdown(data), end="")
        return 0
    if args.command == "list":
        for path in sorted((ROOT / "cases").glob("*/case.json")):
            case = load_case(path.parent.name)
            m = case["manifest"]
            print(f"{m['id']}@{m['version']}\t{', '.join(m['tags'])}\t{m['title']}")
        return 0
    if args.command == "validate":
        suite, cases = load_suite(args.suite)
        print(json.dumps({"suite": suite, "cases": [{"id": c['manifest']['id'], "fingerprint": c['fingerprint']} for c in cases]}, indent=2))
        return 0
    if args.command == "compare":
        args.output = args.output or ROOT / "comparisons" / new_id("compare")
        validate_output_directory(ROOT.parent, args.output)
        result = compare(args.baseline, args.candidate, args.output)
        print(str(args.output.resolve()))
        print(json.dumps(result["counts"]))
        return 0
    if args.command == "reverify":
        TrialLayout.read(args.trial)
        receipt = read_json(args.trial / "receipt.json")
        case = load_case(receipt["case_id"], case_dir=args.trial / "case")
        if case["fingerprint"] != receipt["case_fingerprint"]:
            raise ValueError("frozen case content changed")
        from .storage import copy_tree, tree_hash
        original = read_json(args.trial / "outcome.json")
        if original.get('product_retained') is False:
            raise ValueError('this trial retained reports only; temporary review source was cleaned. Rerun the pinned case to review it again')
        if tree_hash(args.trial / "product") != original["product_sha256"]:
            raise ValueError("saved product changed")
        args.output = new_verification(args.trial, args.output)
        with tempfile.TemporaryDirectory(prefix="nekocode-eval-verify-") as temporary:
            workspace = Path(temporary) / "workspace"
            copy_tree(args.trial / "product", workspace)
            result = verify(case, workspace, args.output.resolve() / "results", (args.trial / "answer.txt").read_text())
        write_json(args.output / "source.json", {"trial": str(args.trial.resolve()), "case_fingerprint": case["fingerprint"], "product_sha256": original["product_sha256"]})
        print(str(args.output.resolve()))
        return 0 if result["product_passed"] else 1
    if args.command == "verify":
        result = verify(load_case(args.case), args.workspace, args.output.resolve(), args.answer.read_text() if args.answer else "")
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 0 if result["product_passed"] else 1
    if not args.allow_user_config:
        parser.error("real runs require --allow-user-config; the standalone evaluator never silently uses application credentials/plugins")
    if args.config_fingerprint and (len(args.config_fingerprint) != 64 or any(c not in "0123456789abcdef" for c in args.config_fingerprint)):
        parser.error("--config-fingerprint must be a lowercase SHA256, not configuration text")
    binary = args.binary.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error("--binary must be an executable file")
    if args.cases:
        if len(set(args.cases)) != len(args.cases):
            parser.error("duplicate case selection")
        cases = [load_case(name) for name in args.cases]
        suite = {"id": "selection", "version": 1, "cases": args.cases}
    else:
        suite, cases = load_suite(args.suite)
    directory = args.output.resolve() / new_id("run")
    directory.mkdir(parents=True, exist_ok=False)
    config = configuration_receipt(args.telemetry_root)
    config_hash = args.config_fingerprint or config["sha256"]
    (directory / "source").mkdir()
    write_json(directory / "source/config-receipt.json", config)
    run_data = {"schema_version": 1, "layout_version": 2, "label": args.label, "run_id": directory.name, "suite": suite, "trials": [], "trial_specs": {}, "status": "running", "source": {"directory": "source", **source_receipt(ROOT.parent)}}
    write_json(directory / "run.json", run_data)
    failed = False
    print(str(directory), flush=True)
    try:
        for repeat in range(1, args.repeat + 1):
            for case in cases:
                relative = "trials/" + new_id(case["manifest"]["id"])
                run_data["trials"].append(relative)
                run_data["trial_specs"][relative] = {"case_id": case["manifest"]["id"], "repeat": repeat}
                write_json(directory / "run.json", run_data)
                outcome = run_trial(case, binary, args.model, args.mode, args.telemetry_root, directory / relative, repeat,
                                    args.interaction_policy, config_hash)
                failed |= not outcome["success"]
                print(json.dumps({"trial": relative, "success": outcome["success"], "stop": outcome["stop_reason"]}), flush=True)
        run_data["status"] = "finished"
    except BaseException:
        run_data["status"] = "interrupted"
        raise
    finally:
        write_json(directory / "run.json", run_data)
    summary = []
    for relative in run_data["trials"]:
        result = read_json(directory / relative / "outcome.json")
        summary.append({"trial": relative, "case_id": run_data["trial_specs"][relative]["case_id"],
                        "success": result["success"], "stop": result["stop_reason"], "cost": result["cost"],
                        "required_passed": result["verification"]["required_passed"], "required_total": result["verification"]["required_total"]})
    write_json(directory / "summary.json", {"schema_version": 1, "run_id": directory.name, "trials": summary})
    lines = ["# Task evaluation run", "", directory.name, "", "| Case | Delivery | Required checks | End | Model calls | Total tokens |", "|---|---|---:|---|---:|---:|"]
    lines += [f"| {r['case_id']} | {r['success']} | {r['required_passed']}/{r['required_total']} | {r['stop']} | {r['cost']['model_calls']} | {r['cost']['total_tokens']} |" for r in summary]
    lines += ["", "Independent product checks, delivery settlement and telemetry integrity are separate. See each trial report and trace/."]
    (directory / "report.md").write_text("\n".join(lines) + "\n")
    return int(failed)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError) as exc:
        print(f"eval error: {exc}", file=sys.stderr)
        sys.exit(2)
