import json
import os
import shutil
from pathlib import Path
import runpy
import tempfile
import unittest
import zipfile
from unittest.mock import patch

from agent_eval.catalog import load_case


class FieldAtlasOracleTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.case = load_case("field-atlas")
        cls.oracle = runpy.run_path(str(cls.case["paths"]["verifier"]))

    def test_docs_need_demo_arguments_and_design_topics(self):
        check = self.oracle["check_docs"]
        commands = ["python3 -m atlas " + cmd for cmd in ("plan", "build", "pack")]
        design = "逐项语言回退，沿依赖传播更新，发布失败保留旧版。"
        with self.assertRaisesRegex(ValueError, "two path arguments"):
            check("\n".join(commands), design)
        readme = "\n".join(cmd + ' "path one" output' for cmd in commands)
        with self.assertRaisesRegex(ValueError, "locale fallback"):
            check(readme, "TODO")
        check(readme, design)
        check(readme.replace(' "path one"', " " + chr(92) + '\n  "path one"'), design)
        with self.assertRaisesRegex(ValueError, "two path arguments"):
            check(readme.replace("output", "output extra"), design)

    def test_docs_count_shell_arguments_after_redirection(self):
        check = self.oracle["check_docs"]
        def examples(arguments):
            return "\n".join(f"python3 -m atlas {op} {arguments}" for op in ("plan", "build", "pack"))
        for arguments in ('src dest > report.json', '2>errors src dest>>"report file"',
                          'src >report dest', '"src>file" ">"', r'src\ file dest'):
            with self.subTest(arguments=arguments):
                check(examples(arguments), "fallback dependency rollback")
        for arguments in ('> report.json', 'src >report', 'src dest >', 'src dest >log extra'):
            with self.subTest(arguments=arguments), self.assertRaisesRegex(ValueError, "two path arguments"):
                check(examples(arguments), "fallback dependency rollback")

    def test_empty_project_has_no_credit(self):
        with tempfile.TemporaryDirectory() as tmp:
            checks = self.oracle["verify"](Path(tmp))
        self.assertEqual({c["id"] for c in checks}, {r["id"] for r in self.case["manifest"]["requirements"]})
        self.assertTrue(all(c["status"] == "failed" for c in checks))

    def test_fallback_oracle_distinguishes_recursive_locale(self):
        cli = Path(__file__).parent / "oracles/field-atlas/atlas.py"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            shutil.copyfile(cli, root / "atlas.py")
            src = root / "src"
            self.oracle["seed"](src)
            for broken in (False, True):
                dest = root / ("broken" if broken else "correct")
                with self.subTest(broken=broken), patch.dict(os.environ, {"ATLAS_TEST_LOSE_LOCALE": str(int(broken))}):
                    if broken:
                        with self.assertRaisesRegex(ValueError, "fallback fr"):
                            self.oracle["probe"]("fallback", root, src, dest)
                    else:
                        self.oracle["probe"]("fallback", root, src, dest)
                    guide = json.loads((dest / "pages/fr/guide.json").read_text())
                    self.assertEqual(guide["lines"], ["α <&>" if broken else "été 🌱", "[Feuille](leaf.json)", "été 🌱"])

    def test_plan_rejects_creation_even_with_correct_delta(self):
        probe = self.oracle["probe"]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            def mutating_plan(project, op, src, dest, **kwargs):
                dest.mkdir()
                return dict(written=sorted(["index.json"] + [f"pages/{lang}/{name}.json"
                    for lang in ("en","fr") for name in ("guide","middle","leaf","spare")]), removed=[])
            with patch.dict(probe.__globals__, run_cli=mutating_plan):
                with self.assertRaisesRegex(ValueError, "plan created"):
                    probe("plan",root,root/"src",root/"out")

    def test_wrong_member_size_rejected_before_decompression(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "release.zip"
            with zipfile.ZipFile(path, "w") as archive:
                archive.writestr("index.json", b"wrong length")
            with patch.object(zipfile.ZipFile, "open", side_effect=AssertionError("unexpected read")):
                with self.assertRaisesRegex(ValueError, "member size"):
                    self.oracle["check_archive"](path, ["index.json"], {"index.json": b"[]"})

    def test_archive_rejects_pack_time_in_default_zip_entries(self):
        probe = self.oracle["probe"]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            def cli(project, op, src, dest):
                if op == "build":
                    names = ["index.json"] + [f"pages/{lang}/{page}.json" for lang in ("en", "fr")
                                             for page in ("guide", "middle", "leaf", "spare")]
                    for name in names:
                        (dest/name).parent.mkdir(parents=True, exist_ok=True)
                        (dest/name).write_bytes(b"{}")
                    return {}
                with zipfile.ZipFile(dest, "w") as archive:
                    for path in sorted(p for p in src.rglob("*") if p.is_file()):
                        archive.writestr(path.relative_to(src).as_posix(), path.read_bytes())
                    return {"entries": len(archive.namelist())}
            with patch.dict(probe.__globals__, run_cli=cli):
                with self.assertRaisesRegex(ValueError, "zip depends on timestamps"):
                    probe("archive", root, root/"src", root/"out")
