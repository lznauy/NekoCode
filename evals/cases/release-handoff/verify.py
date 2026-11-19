import json
from pathlib import Path
import re
import sys

workspace, output = map(Path, sys.argv[1:3])
checks = []

def read(name):
    path = workspace / name
    return path.read_text(errors="replace") if path.is_file() and not path.is_symlink() else ""

def check(name, passed, evidence):
    checks.append(dict(id=name, status="passed" if passed else "failed", evidence=evidence))

def writers(events, original_workspace):
    authors = {"ops.md": set(), "support-draft.md": set()}
    with events.open() as rows:
        for line in rows:
            event = json.loads(line)
            payload = event.get("payload")
            if event.get("type") != "tool_completed" or not isinstance(payload, dict):
                continue
            child = payload.get("subagent_id")
            if not child or payload.get("tool_name") != "write" or payload.get("is_error"):
                continue
            args = payload.get("input", {})
            path = Path(args.get("path", ""))
            if path.is_absolute():
                if not path.is_relative_to(original_workspace):
                    continue
                path = path.relative_to(original_workspace)
            name = str(path)
            if name in authors and args.get("content", "").strip() and args["content"].strip() == read(name).strip():
                authors[name].add(child)
    return any(a != b for a in authors["ops.md"] for b in authors["support-draft.md"])

if len(sys.argv) > 3 and Path(sys.argv[3]).is_file():
    check("workers", writers(Path(sys.argv[3]), Path(sys.argv[4]) if len(sys.argv) > 4 else workspace),
          "Distinct child IDs, successful top-level writes and matching delivered draft contents")
else:
    checks.append(dict(id="workers", status="not_run", evidence="Runtime event journal unavailable"))
ops, support, final = map(read, ["ops.md", "support-draft.md", "HANDBOOK.md"])
sources_intact = all(read(p.name) == p.read_text() for p in Path(__file__).with_name("fixture").glob("*.md"))
check("drafts", sources_intact and all(x in ops for x in ("atlasctl shadow", "atlasctl promote", "eu-west")) and
      all(x in support for x in ("Atlas 维护窗口", "CHG-418", "atlas-duty@example.org")), "Source-specific draft facts")
tokens = ["03:40 UTC", "eu-west", "atlasctl shadow", "atlasctl promote", "atlasctl revert", "CHG-418",
          "Atlas 维护窗口", "atlas-duty@example.org"]
def customer_impact(text):
    # Match each policy with its own activity. Both word orders occur in notices.
    available = r"(?<!不)(?:允许|可以|可继续|仍可|可用|可|照常|正常|不受影响|保持开放|开放|支持)"
    paused = r"(?:暂停|禁止|不可用|不能|无法|不允许|不支持|停止|关闭|不可)"
    def states(activity, policy):
        return re.search(rf"{policy}\s*(?:进行)?(?:数据)?{activity}|{activity}(?:功能|服务|操作)?\s*(?:将|仍|暂时|暂)?{policy}", text)
    return states("查询", available) and states("导入", paused) and not (
        states("查询", paused) or states("导入", available))

check("handbook", all(x in final for x in tokens) and final.index("atlasctl shadow") < final.index("atlasctl promote")
      and customer_impact(final)
      and re.search(r"(?<![0-9.])12\s*(?:分钟|min\b)", final) and re.search(r"(?:低于|小于|<)\s*80(?![0-9]|\.[0-9])", final)
      and all(any(word in line for word in ("旧", "作废", "废止", "演练", "过期", "取消", "原定"))
              for line in final.splitlines() if "01:00 UTC" in line)
      and not re.search(r"\b20\d{2}[-/]\d{1,2}[-/]\d{1,2}\b", final), "Signed time only (no invented calendar date), operational sequence, threshold and support facts; prose quality needs review")
(output / "result.json").write_text(json.dumps(dict(schema_version=1, checks=checks)))
