import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
import time
import zipfile


def require(condition, message):
    if not condition:
        raise ValueError(message)


def run_cli(project, op, *paths, error=None):
    result = subprocess.run([sys.executable, "-B", "-m", "atlas", op, *paths],
                            cwd=project, capture_output=True, text=True, timeout=10)
    require(result.returncode == (2 if error else 0),
            f"{op}: exit {result.returncode}, stdout={result.stdout[-200:]}, stderr={result.stderr[-200:]}")
    reply = json.loads(result.stdout)
    require(isinstance(reply, dict), f"{op} response must be an object")
    if error:
        require(reply.get("error") == error, f"{op}: wanted {error}, got {reply}")
    return reply


def save(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")


def page(name, title, *blocks):
    return {"id": name, "variants": {"en": {"title": title, "blocks": list(blocks)}}}


def seed(src):
    pages = src / "pages"
    pages.mkdir(parents=True)
    save(src / "site.json", {"locales": ["fr", "en"], "default": "en"})
    leaf = page("leaf", "Leaf", {"text": "α <&>"})
    leaf["variants"]["fr"] = {"title": "Feuille", "blocks": [{"text": "été 🌱"}]}
    save(pages / "x.json", leaf)
    save(pages / "y.json", page("middle", "Middle", {"include": "leaf"}))
    save(pages / "z.json", page("guide", "Guide", {"include": "middle"},
                                {"link": "leaf"}, {"include": "leaf"}))
    save(pages / "a.json", page("spare", "Spare", {"text": ""}))


def snapshot(dest):
    return {p.relative_to(dest).as_posix(): (p.read_bytes(), p.stat().st_mtime_ns)
            for p in dest.rglob("*") if p.is_file()}


def files(state):
    return {name: data for name, (data, _) in state.items()}


def delta(reply, writes, removals=()):
    require(reply.get("written") == sorted(writes) and reply.get("removed") == sorted(removals),
            f"unexpected delta: {reply}")


def probe(kind, project, src, dest):
    def cli(op, *args, **kwargs):
        return run_cli(project, op, *(args or (src, dest)), **kwargs)

    pages = src / "pages"
    expected = ["index.json"] + [f"pages/{lang}/{name}.json" for lang in ("en", "fr")
                                for name in ("guide", "middle", "leaf", "spare")]
    if kind == "plan":
        delta(cli("plan"), expected)
        require(not dest.exists(), "plan created destination")
    if kind == "compile":
        save(pages / "a.json", page("spare", "Spare", {"link": "guide"}, {"text": ""}))
        save(pages / "z.json", page("guide", "Guide", {"link": "spare"}))
    first = cli("build")
    if kind == "compile":
        state = snapshot(dest)
        require(set(state) == set(expected), "unexpected paths in full build")
        index = json.loads(state["index.json"][0])
        entries = []
        for lang in ("en", "fr"):
            for name, title in (("guide","Guide"), ("leaf","Feuille" if lang=="fr" else "Leaf"),
                                ("middle","Middle"), ("spare","Spare")):
                path = f"pages/{lang}/{name}.json"
                entries.append(dict(id=name, locale=lang, title=title, path=path))
                lines = {"guide":["[Spare](spare.json)"], "spare":["[Guide](guide.json)", ""],
                         "leaf":["été 🌱" if lang=="fr" else "α <&>"],
                         "middle":["été 🌱" if lang=="fr" else "α <&>"]}[name]
                require(json.loads(state[path][0]) == dict(id=name, locale=lang, title=title, lines=lines), path)
        require(index == entries, "index fields or ordering")
    elif kind == "fallback":
        for lang, line, title in (("fr","été 🌱","Feuille"), ("en","α <&>","Leaf")):
            value = json.loads((dest/f"pages/{lang}/guide.json").read_text())
            require(value == dict(id="guide", locale=lang, title="Guide",
                                  lines=[line, f"[{title}](leaf.json)", line]), f"fallback {lang}: {value}")
    elif kind == "plan":
        delta(first, expected)
        original = snapshot(dest)
        value = json.loads((pages / "x.json").read_text())
        (pages/"x.json").unlink()
        (pages/"renamed.json").write_text(json.dumps(dict(reversed(list(value.items()))),indent=3))
        delta(cli("plan"), [])
        delta(cli("build"), [])
        require(original == snapshot(dest), "unchanged bytes or mtimes were rewritten")
    elif kind == "incremental":
        original = snapshot(dest)
        leaf = json.loads((pages/"x.json").read_text())
        leaf["variants"]["fr"]["blocks"] = [{"text":"deuxième"}]
        save(pages/"x.json", leaf)
        (pages/"a.json").unlink()
        changed = ["index.json"] + [f"pages/fr/{name}.json" for name in ("leaf","middle","guide")]
        removed = [f"pages/{lang}/spare.json" for lang in ("en","fr")]
        delta(cli("plan"), changed, removed)
        require(original == snapshot(dest), "plan modified old release")
        delta(cli("build"), changed, removed)
        new = snapshot(dest)
        for path in original.keys() - set(changed + removed):
            require(new[path] == original[path], f"unnecessary rewrite: {path}")
        clean = dest.parent/"clean"
        cli("build", src, clean)
        require(files(new) == files(snapshot(clean)), "incremental differs from clean")
        require(json.loads(new["pages/fr/guide.json"][0])["lines"] ==
                ["deuxième","[Feuille](leaf.json)","deuxième"], "transitive change missed")
    elif kind == "rollback":
        original = snapshot(dest)
        # A validation failure must withhold a legitimate pending edit too.
        old_spare = (pages/"a.json").read_bytes()
        save(pages/"a.json", page("spare", "Pending", {"text":"withhold me"}))
        for bad, error in [(page("bad","Broken",{"include":"bad"}),"cycle"),
                           (page("bad","Broken",{"link":"unknown"}),"missing"),
                           (page("bad","Broken",{"text":"one","include":"leaf"}),"invalid"),
                           (page("guide","Duplicate"),"invalid"),
                           (page("../bad","Bad"),"invalid")]:
            save(pages/"bad.json", bad)
            cli("build", error=error)
            require(snapshot(dest) == original, f"{error} changed the previous release")
        (pages/"bad.json").unlink()
        (pages/"a.json").write_bytes(old_spare)
        delta(cli("build"), [])
    elif kind == "repair":
        rel = "pages/fr/guide.json"
        original = snapshot(dest)
        (dest/rel).write_bytes(b"damaged")
        delta(cli("plan"), [rel])
        delta(cli("build"), [rel])
        require(files(snapshot(dest)) == files(original), "damage survived rebuild")
    elif kind == "archive":
        state = files(snapshot(dest))
        one, two = (dest.parent/n for n in ("one.zip","two.zip"))
        reply = cli("pack", dest, one)
        require(reply.get("entries") == len(expected), "wrong entries count")
        check_archive(one, expected, state)
        for name in state:
            os.utime(dest/name, (1700000000,1700000000))
        # ZIP timestamps have two-second resolution.
        time.sleep(2.1)
        cli("pack", dest, two)
        with one.open("rb") as a, two.open("rb") as b:
            while True:
                chunk = a.read(65536)
                require(chunk == b.read(65536), "zip depends on timestamps or output name")
                if not chunk:
                    break


def check_archive(path, expected, state):
    with zipfile.ZipFile(path) as z:
        require(z.namelist() == sorted(expected), "incorrect archive names/order")
        for name in expected:
            info, data = z.getinfo(name), state[name]
            require(info.file_size == len(data), "archive member size differs from release")
            with z.open(info) as member:
                require(member.read(len(data) + 1) == data, "archive bytes differ from release")


# Preserve shell quoting until redirects have been distinguished from paths.
SHELL_WORD = re.compile(r"""(?:[^\s\\'"<>&|;()]+|'[^']*'|"(?:\\.|[^"\\])*"|\\.)+""")
REDIRECT = re.compile(r"(?:[0-9]*(?:>>|>\||>&|<>|<&|[<>])|&>>?)")


def example_args(command):
    args = []
    rest = command.lstrip()
    while rest and not rest.startswith("#"):
        redirect = REDIRECT.match(rest)
        if redirect:
            rest = rest[redirect.end():].lstrip()
            if rest.startswith("#"):
                return []
        word = SHELL_WORD.match(rest)
        if not word:
            return []
        if not redirect:
            args.extend(shlex.split(word[0]))
        rest = rest[word.end():].lstrip()
    return args


def check_docs(readme, design):
    readme = re.sub(r"\\\r?\n", "", readme)
    examples = set()
    for match in re.finditer(r"\bpython3\s+-m\s+atlas[ \t]+([^\n`]+)", readme):
        args = example_args(match[1])
        if len(args) == 3 and all(args[1:]):
            examples.add(args[0])
    require({"plan", "build", "pack"} <= examples, "README needs two path arguments for each CLI example")
    for topic, pattern in (
            ("locale fallback", r"回退|继承|回落|默认语言|fallback|fall[ -]?back|inherit|default locale"),
            ("dependency propagation", r"依赖|传播|传递|dependenc|propagat|transitiv"),
            ("failed publication", r"失败|回滚|原子|旧版|fail|rollback|roll back|atomic|previous release")):
        require(re.search(pattern, design, re.I), f"design needs to discuss {topic}")


def delivery(project):
    fixture = Path(__file__).parent/"fixture"
    for name in ("CONTRACT.md","example/input.json"):
        require((project/name).read_bytes() == (fixture/name).read_bytes(), f"altered {name}")
    check_docs((project/"README.md").read_text(), (project/"docs/design.md").read_text())
    driver = """import os,sys,unittest,json
sys.path.insert(0,os.getcwd())
suite=unittest.defaultTestLoader.discover('tests')
r=unittest.TestResult()
suite.run(r)
print(json.dumps([r.wasSuccessful(),r.testsRun-len(r.skipped)-len(r.expectedFailures)]))"""
    result = subprocess.run([sys.executable,"-IB","-c",driver],cwd=project,capture_output=True,text=True,timeout=45)
    require(result.returncode == 0 and result.stdout.strip(), "test discovery failed")
    success, count = json.loads(result.stdout.strip().splitlines()[-1])
    require(success and count >= 8, f"submitted tests: success={success}, passed={count}")
    return f"{count} tests passed, documentation and seeds present"


def verify(project):
    checks = []
    for kind in ("compile","fallback","plan","incremental","rollback","repair","archive","delivery"):
        try:
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                src, dest = root/"输入 pages",root/"field release"
                seed(src)
                if kind == "delivery":
                    evidence = delivery(project)
                else:
                    probe(kind,project,src,dest)
                    evidence = f"{kind} verified through fresh CLI processes"
            status = "passed"
        except (OSError, ValueError, KeyError, TypeError, IndexError, subprocess.SubprocessError, zipfile.BadZipFile) as exc:
            evidence, status = str(exc), "failed"
        checks.append(dict(id=kind,status=status,evidence=evidence[:800]))
    return checks


if __name__ == "__main__":
    project, output = map(Path,sys.argv[1:3])
    (output/"result.json").write_text(json.dumps(dict(schema_version=1,checks=verify(project))))
