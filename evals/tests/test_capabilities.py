import json
from pathlib import Path
import shutil
import tempfile
import unittest

from agent_eval.catalog import load_case
from agent_eval.verification import verify


class CapabilityChecks(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.ws = self.root / "product"

    def load(self, name):
        case = load_case(name)
        shutil.copytree(case["paths"]["fixture"], self.ws)
        return case

    def test_depot_v1_reader_cannot_pass_project_checks(self):
        case = self.load("depot-ledger")
        result = verify(case, self.ws, self.root / "results")
        self.assertIsNone(result["error"])
        self.assertEqual(result["required_total"], 10)
        self.assertEqual(result["required_passed"], 0)

    def test_inventory_rejects_release_claim_for_prototype(self):
        case = self.load("inventory-release")
        (self.ws / "STATUS.md").write_text("aggregate PASS tested\ncsv PASS tested\natomic PASS tested")
        result = verify(case, self.ws, self.root / "results")
        self.assertIsNone(result["error"])
        self.assertTrue(all(c["status"] == "failed" for c in result["checks"]))

    def test_ledger_seed_distinguishes_streams_from_recovery(self):
        case = self.load("ledger-resume")
        result = verify(case, self.ws, self.root / "results")
        self.assertIsNone(result["error"])
        self.assertEqual({c["id"] for c in result["checks"] if c["status"] == "passed"}, {"streams"})

    def test_writer_attribution_requires_distinct_successful_authors(self):
        case = self.load("release-handoff")
        drafts = {"ops.md": "eu-west: atlasctl shadow, atlasctl promote",
                  "support-draft.md": "Atlas 维护窗口 CHG-418 atlas-duty@example.org"}
        for name, content in drafts.items():
            (self.ws / name).write_text(content)
        trace = self.root / "protocol.jsonl"
        original = self.root / "agent-workspace"
        scenarios = [
            (Path("."), True, False, "passed"), (original, True, False, "passed"),
            (Path("elsewhere"), True, False, "failed"), (original.parent, True, False, "failed"),
            (original, False, False, "failed"), (original, True, True, "failed"),
        ]
        for index, (parent, distinct, failed, expected) in enumerate(scenarios):
            frames = []
            for i, (path, content) in enumerate(drafts.items()):
                child = str(i) if distinct else "same"
                event = dict(type="tool_completed", payload=dict(subagent_id=child, tool_name="write",
                             is_error=failed, input=dict(path=str(parent / path), content=content)))
                frames.append(json.dumps(event))
            trace.write_text("\n".join(frames))
            result = verify(case, self.ws, self.root / str(index), events_path=trace, original_workspace=original)
            self.assertEqual(result["checks"][0]["status"], expected)
        result = verify(case, self.ws, self.root / "missing-trace")
        self.assertEqual(result["checks"][0]["status"], "not_run")

    def test_handbook_requires_correct_customer_impact(self):
        case = self.load("release-handoff")
        handbook = ("03:40 UTC eu-west atlasctl shadow 连续12分钟低于80 atlasctl promote "
                    "atlasctl revert CHG-418 Atlas 维护窗口 atlas-duty@example.org。")
        for i, (notice, expected) in enumerate([
                ("允许查询，暂停导入。", "passed"), ("查询正常，导入不可用。", "passed"),
                ("仍可查询，不允许导入。", "passed"), ("可查询，不支持导入。", "passed"),
                ("查询服务不受影响，暂不能进行数据导入。", "passed"),
                ("暂停查询，允许导入。", "failed"),
                ("允许查询，允许导入。", "failed"), ("查询，导入。", "failed")]):
            with self.subTest(notice=notice):
                (self.ws / "HANDBOOK.md").write_text(handbook + notice)
                result = verify(case, self.ws, self.root / str(i))
                self.assertEqual(next(c["status"] for c in result["checks"] if c["id"] == "handbook"), expected)

    def test_handbook_rejects_different_threshold_or_duration(self):
        case = self.load("release-handoff")
        final = self.ws / "HANDBOOK.md"
        for i, (duration, threshold) in enumerate([(112, 80), (12, 800), (12, 80.5)]):
            final.write_text(f"03:40 UTC eu-west atlasctl shadow 连续{duration}分钟低于{threshold} "
                             "atlasctl promote atlasctl revert CHG-418 Atlas 维护窗口 "
                             "atlas-duty@example.org。允许查询，暂停导入。")
            result = verify(case, self.ws, self.root / str(i))
            self.assertIsNone(result["error"], result)
            self.assertEqual(next(c["status"] for c in result["checks"] if c["id"] == "handbook"), "failed")

    def test_architecture_requires_correct_location_and_frozen_tree(self):
        from agent_eval.storage import tree_hash

        case = load_case("go-embed-path")
        facts = [
            ("matching", ["a.txt"], "src/cmd/go/internal/load/pkg.go", "if isBadEmbedName(name) || other {"),
            ("handoff", ["Patterns", "Files"], "src/cmd/go/internal/work/exec.go", "embed.Patterns = p.Internal.Embed"),
            ("compile", "build_error", "src/cmd/compile/internal/staticdata/embed.go", "if len(list) > 1 {"),
            ("lookup", "fs.ErrNotExist", "src/embed/embed.go", "return nil, &fs.PathError{Err: fs.ErrNotExist}"),
        ]
        answer = {}
        for key, value, file, line in facts:
            path = self.ws / "repo" / file
            path.parent.mkdir(parents=True)
            path.write_text("// header\n" + line)
            answer[key] = dict(value=value, refs=[dict(file=file, line=2)], explanation="应用实现分支所得")
        digest = tree_hash(self.ws)
        self.assertTrue(verify(case, self.ws, self.root / "pass", json.dumps(answer), input_sha256=digest)["product_passed"])
        answer["lookup"]["refs"][0]["line"] = 1
        (self.ws / "new").write_text("changed")
        result = verify(case, self.ws, self.root / "fail", json.dumps(answer), input_sha256=digest)
        self.assertEqual({c["id"] for c in result["checks"] if c["status"] == "failed"}, {"lookup", "unchanged"})
