import collections
from pathlib import Path
import statistics

from .storage import read_json, write_json


def trials(run):
    run = Path(run)
    manifest = read_json(run / "run.json")
    result = {}
    for relative in manifest["trials"]:
        directory = (run / relative).resolve()
        if not directory.is_relative_to(run.resolve()):
            raise ValueError("trial path escapes run")
        if not (directory / "receipt.json").exists():
            spec = manifest.get("trial_specs", {}).get(relative)
            if not spec:
                raise ValueError("incomplete trial without identity in run manifest")
            receipt = spec
        else:
            receipt = read_json(directory / "receipt.json")
        outcome = read_json(directory / "outcome.json") if (directory / "outcome.json").exists() else None
        result[(receipt["case_id"], receipt["repeat"])] = (receipt, outcome)
    return manifest, result


def delivery_cost(outcomes):
    """Amortize all comparable attempts, including failures, over accepted deliveries."""
    successes = sum(o["success"] for o in outcomes)
    result = {"attempts": len(outcomes), "deliveries": successes}
    complete = all(o.get("integrity", {}).get("calls_observed") and
                   o["integrity"].get("all_call_ids_present") for o in outcomes)
    for metric in ("model_calls", "total_tokens"):
        values = [o["cost"].get(metric) for o in outcomes]
        known = complete and all(v is not None for v in values)
        result[metric + "_per_delivery"] = sum(values) / successes if successes and known else None
    return result


def compare(baseline, candidate, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=False)
    base_run, base = trials(baseline)
    cand_run, cand = trials(candidate)
    rows = []
    comparable = collections.defaultdict(lambda: {"baseline": [], "candidate": []})
    for key in sorted(base.keys() | cand.keys()):
        row = {"case_id": key[0], "repeat": key[1], "classification": "unmatched", "reasons": [], "cost_delta": None}
        if key not in base or key not in cand:
            row["reasons"] = ["case/repeat exists in only one run"]
            rows.append(row)
            continue
        br, bo = base[key]
        cr, co = cand[key]
        if bo is None or co is None:
            row["classification"] = "incomparable"
            row["reasons"] = ["trial was interrupted before its outcome was finalized"]
            rows.append(row)
            continue
        for field in ["case_fingerprint", "model_requested", "mode", "interaction_policy", "external_config_fingerprint", "environment", "budgets", "telemetry_adapter", "observed_model_fingerprint"]:
            if br.get(field) != cr.get(field):
                row["reasons"].append(f"{field} differs")
        for role in br.get("auxiliary_models", {}).keys() & cr.get("auxiliary_models", {}).keys():
            if br["auxiliary_models"][role] != cr["auxiliary_models"][role]:
                row["reasons"].append(f"auxiliary model/effort differs: {role}")
        version = br.get('evaluation_contract_version')
        other_version = cr.get('evaluation_contract_version')
        if type(version) is not int or version < 1 or type(other_version) is not int or version != other_version:
            row['reasons'].append('evaluation_contract_version missing, invalid or differs')
        for field in ["telemetry_adapter"]:
            if not br.get(field) or not cr.get(field):
                row["reasons"].append(f"{field} missing")
        if not br.get("observed_model_fingerprint") or not cr.get("observed_model_fingerprint"):
            row["reasons"].append("actual model configuration was not observed")
        if not br.get("external_config_fingerprint") or not cr.get("external_config_fingerprint"):
            row["reasons"].append("external configuration was not fingerprinted")
        if bo.get("verification", {}).get("error") or co.get("verification", {}).get("error"):
            row["reasons"].append("verifier failed; product outcome unscored")
        if row["reasons"]:
            row["classification"] = "incomparable"
        else:
            comparable[key[0]]["baseline"].append(bo)
            comparable[key[0]]["candidate"].append(co)
            bs, cs = bo["success"], co["success"]
            row["classification"] = {(False, True): "new_success", (True, False): "regression", (True, True): "both_success", (False, False): "both_failure"}[(bs, cs)]
            if bs and cs and all(o.get("integrity", {}).get("calls_observed") and o.get("integrity", {}).get("all_call_ids_present") for o in [bo, co]):
                row["cost_delta"] = {}
                for metric in ["model_calls", "total_tokens", "input_tokens", "output_tokens", "fresh_tokens", "cached_tokens", "tool_calls", "tool_requests", "repeated_tool_requests", "tool_rounds", "tool_rounds_before_first_read", "tool_started", "tool_blocked", "tool_result_errors", "context_snapshot_polls", "same_image_rechecks", "image_rate_limits"]:
                    bv, cv = bo["cost"].get(metric), co["cost"].get(metric)
                    row["cost_delta"][metric] = cv - bv if bv is not None and cv is not None else None
                row["cost_delta"]["elapsed_s"] = co["elapsed_s"] - bo["elapsed_s"]
        row["baseline"] = {"success": bo["success"], "stop": bo["stop_reason"], "required_passed": bo["verification"]["required_passed"], "cost": bo["cost"]}
        row["candidate"] = {"success": co["success"], "stop": co["stop_reason"], "required_passed": co["verification"]["required_passed"], "cost": co["cost"]}
        rows.append(row)
    deltas = [r["cost_delta"] for r in rows if r["cost_delta"] is not None]
    # Every aggregate carries its own denominator: missing metrics are not zeros.
    aggregate = {}
    for metric in ["model_calls", "total_tokens", "fresh_tokens", "output_tokens", "tool_calls", "tool_requests", "repeated_tool_requests", "tool_rounds", "tool_rounds_before_first_read", "tool_started", "tool_blocked", "tool_result_errors", "context_snapshot_polls", "same_image_rechecks", "image_rate_limits", "elapsed_s"]:
        values = [d[metric] for d in deltas if d.get(metric) is not None]
        aggregate[metric] = {"paired_success_samples": len(values), "median_candidate_minus_baseline": statistics.median(values) if values else None}
    result = {"schema_version": 1, "baseline_run": base_run["run_id"], "candidate_run": cand_run["run_id"],
              "counts": dict(collections.Counter(r["classification"] for r in rows)), "pairs": rows,
              "successful_pair_cost": aggregate,
              "cost_per_delivery": {case: {side: delivery_cost(outcomes) for side, outcomes in group.items()}
                                    for case, group in comparable.items()},
              "limitations": ["A small number of repeats does not establish statistical significance.",
                              "External config fingerprints are operator assertions; requested model names do not pin provider deployments.",
                              "Only successful matched trials contribute cost deltas; timeouts remain delivery failures.",
                              "Unknown/rotated telemetry can undercount; inspect integrity before interpreting cost."]}
    write_json(output / "comparison.json", result)
    lines = ["# Harness comparison", "", f"Baseline: {base_run['run_id']}; candidate: {cand_run['run_id']}", "", "| Case | Repeat | Outcome | Reasons |", "|---|---:|---|---|"]
    lines += [f"| {r['case_id']} | {r['repeat']} | {r['classification']} | {'; '.join(r['reasons'])} |" for r in rows]
    lines += ["", "## Cost on matched successful trials", "", "| Metric | Samples | Median delta (candidate − baseline) |", "|---|---:|---:|"]
    lines += [f"| {k} | {v['paired_success_samples']} | {v['median_candidate_minus_baseline']} |" for k, v in aggregate.items()]
    lines += ["", "## Cost per accepted delivery, including failed attempts", "",
              "| Case | Version | Delivered / attempts | Model calls / delivery | Tokens / delivery |",
              "|---|---|---:|---:|---:|"]
    for case, versions in result["cost_per_delivery"].items():
        for side, value in versions.items():
            calls = value["model_calls_per_delivery"]
            tokens = value["total_tokens_per_delivery"]
            costs = [format(calls, ".2f") if calls is not None else "—",
                     format(tokens, ",.1f") if tokens is not None else "—"]
            lines.append(f"| {case} | {side} | {value['deliveries']} / {value['attempts']} | " + " | ".join(costs) + " |")
    lines += ["", *["- " + note for note in result["limitations"]]]
    (output / "report.md").write_text("\n".join(lines) + "\n")
    return result
