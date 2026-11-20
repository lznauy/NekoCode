"""Build an isolated Linux runtime for evaluator code and its product subprocesses."""
import functools
import os
from pathlib import Path
import shutil
import subprocess
import sys


@functools.lru_cache(maxsize=16)
def nix_closure(roots):
    # Query the locally installed runtime, never expose the entire Nix store
    # (which can also contain project source and user configuration).
    output = subprocess.check_output(["nix-store", "--query", "--requisites", *roots],
                                     text=True, stderr=subprocess.PIPE, timeout=15)
    paths = output.splitlines()
    if not paths or any(Path(p).parent != Path("/nix/store") for p in paths):
        raise ValueError("invalid Nix runtime closure")
    return paths


def command(case, workspace=None, staging=None, events=None, original_workspace=None):
    bwrap = os.environ.get("NEKOCODE_EVAL_BWRAP") or shutil.which("bwrap")
    if sys.platform != "linux" or not bwrap:
        raise OSError("Linux bubblewrap is required for verification; install bwrap or set NEKOCODE_EVAL_BWRAP")
    if not Path(bwrap).is_absolute():
        raise ValueError("bwrap must be an absolute executable path")
    python = Path(sys.executable).resolve(strict=True)
    args = [bwrap, "--unshare-all", "--unshare-user", "--disable-userns", "--die-with-parent",
            "--new-session", "--cap-drop", "ALL", "--clearenv", "--setenv", "HOME", "/home/verify",
            "--setenv", "LANG", "C.UTF-8", "--setenv", "TMPDIR", "/tmp", "--proc", "/proc", "--dev", "/dev",
            "--tmpfs", "/tmp", "--dir", "/home/verify", "--dir", "/tools", "--setenv", "PATH", "/tools:/usr/bin:/bin"]
    browser = case["manifest"]["id"] in ("snake", "pelican-bicycle", "goal-night-shift")
    tools = {"python3": python, "python": python}
    if shell := shutil.which("sh"):
        tools["sh"] = Path(shell).resolve()
    if browser:
        if node := shutil.which("node"):
            tools["node"] = Path(node).resolve()
        chrome = os.environ.get("CHROME_BIN") or shutil.which("google-chrome") or shutil.which("chromium")
        if chrome and Path(chrome).is_file():
            tools["chrome"] = Path(chrome).resolve()
            args.extend(["--setenv", "CHROME_BIN", "/tools/chrome"])
        # Expose the required module, not the operator's whole NODE_PATH.
        for directory in filter(None, os.environ.get("NODE_PATH", "").split(os.pathsep)):
            module = (Path(directory) / "playwright-core").resolve()
            if (module / "package.json").is_file():
                args.extend(["--ro-bind", str(module), "/modules/playwright-core", "--setenv", "NODE_PATH", "/modules"])
                break

    nix = Path("/nix/store")
    nix_roots = {str(nix / path.relative_to(nix).parts[0]) for path in tools.values() if path.is_relative_to(nix)}
    if nix_roots:
        for path in nix_closure(tuple(sorted(nix_roots))):
            args.extend(["--ro-bind", path, path])
    # FHS distro runtimes and browser fonts. Exclude host /etc, /run, home and
    # general-purpose /usr/local data. Only these runtime directories are shared.
    runtime = ["/usr/bin", "/usr/lib", "/usr/lib64", "/usr/local/lib", "/lib", "/lib64", "/bin"]
    if browser:
        runtime += ["/usr/share/fonts", "/usr/share/fontconfig", "/etc/fonts", "/opt/google/chrome"]
    for path in runtime:
        if Path(path).is_dir():
            args.extend(["--ro-bind", path, path])
    for name, path in tools.items():
        if path.is_relative_to(nix) or any(path.is_relative_to(directory) for directory in runtime):
            args.extend(["--symlink", str(path), "/tools/" + name])
        else:
            args.extend(["--ro-bind", str(path), "/tools/" + name])
    if workspace is None:
        return [*args, "--", "/tools/python3", "-I", "-c", "pass"]
    verifier = case["paths"]["verifier"]
    args.extend(["--ro-bind", str(verifier.parent.resolve()), "/case",
                 "--ro-bind", str(workspace), "/workspace", "--bind", str(staging), "/output"])
    include_events = events is not None and bool({"multi-agent-writing", "session-goal"}.intersection(case["manifest"]["tags"]))
    if include_events:
        if Path(events).is_file():
            args.extend(["--ro-bind", str(Path(events).resolve()), "/events.jsonl"])
    args += ["--chdir", "/output", "--", "/tools/python3", "-I"]
    product = "/workspace"
    if case["manifest"].get("repository") is None:
        # Product tests may write alongside the source. Give them a disposable
        # copy inside the private tmpfs, including for direct `verify`.
        product = "/tmp/product"
        args += ["-c", "import os,shutil,sys; shutil.copytree('/workspace','/tmp/product',symlinks=True,"
                 "ignore=shutil.ignore_patterns('.git','__pycache__')); os.execv(sys.executable,"
                 "[sys.executable,'-I',*sys.argv[1:]])"]
    args += ["/case/" + verifier.name, product, "/output"]
    if include_events:
        args.extend(["/events.jsonl", str(original_workspace or workspace)])
    return args


def preflight(case):
    """Detect missing namespaces/runtime before paying for any model requests."""
    result = subprocess.run(command(case), stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, env={}, timeout=15)
    if result.returncode:
        raise OSError("verifier sandbox unavailable: " + result.stderr.strip()[-500:])
