"""Prepare pinned review inputs only when a repository case is executed."""
import os
from pathlib import Path
import re
import shutil
import subprocess
from urllib.parse import urlsplit

from .storage import digest, tree_hash
from .verification import stop_process


class PreparationError(RuntimeError):
    pass


def validate_repository(value):
    if not isinstance(value, dict) or set(value) != {'url', 'base', 'head'}:
        raise ValueError('repository requires url, base and head')
    if not isinstance(value['url'], str):
        raise ValueError('repository URL must be a string')
    url = urlsplit(value['url'])
    if (url.scheme not in {'https', 'file'} or not url.path or url.username or url.password
            or url.query or url.fragment or any(c.isspace() for c in value['url'])
            or url.scheme == 'https' and not url.netloc or url.scheme == 'file' and url.netloc not in {'', 'localhost'}):
        raise ValueError('repository must use a credential-free HTTPS URL or local file URL')
    for name in ['base', 'head']:
        if not isinstance(value[name], str) or not re.fullmatch(r'[0-9a-f]{40}', value[name]):
            raise ValueError(f'repository {name} must be a full pinned commit SHA')


def run_git(*args, log, capture=False):
    # Preparation must not run operator hooks, URL rewrites, signing programs
    # or credential prompts. Each command owns a process group for timeout cleanup.
    env = {key: value for key, value in os.environ.items() if not key.startswith('GIT_')}
    env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM='1', GIT_TERMINAL_PROMPT='0')
    proc = subprocess.Popen(['git', '-c', 'core.hooksPath=' + os.devnull, *args], env=env,
                            stdout=subprocess.PIPE if capture else log, stderr=log, start_new_session=True)
    try:
        stdout, _ = proc.communicate(timeout=120)
        if proc.returncode:
            raise subprocess.CalledProcessError(proc.returncode, proc.args)
        return stdout
    finally:
        stop_process(proc)


def prepare_repository(case, workspace, log_path):
    source = case['manifest']['repository']
    validate_repository(source)
    workspace = Path(workspace)
    workspace.mkdir(parents=True, exist_ok=False)
    repo = workspace / 'repo'
    with Path(log_path).open('w') as log:
        try:
            run_git('init', '-q', str(repo), log=log)
            run_git('-C', str(repo), 'remote', 'add', 'origin', source['url'], log=log)
            run_git('-C', str(repo), 'fetch', '--no-tags', '--depth=1', 'origin', source['base'], source['head'], log=log)
            run_git('-C', str(repo), 'checkout', '--detach', '--quiet', source['head'], log=log)
            patch = run_git('-C', str(repo), 'diff', '--binary', '--no-ext-diff', '--no-textconv', source['base'], source['head'], capture=True, log=log)
            (workspace / 'review.patch').write_bytes(patch)
        except (OSError, subprocess.SubprocessError) as exc:
            raise PreparationError(f'repository preparation failed ({type(exc).__name__}); see trace/preparation.log') from exc
    # The Agent receives the pinned tree and review diff, not upstream history
    # or a nested Git repository that would become a gitlink in its workspace.
    shutil.rmtree(repo / '.git')
    return {**source, 'diff_sha256': digest(patch), 'input_sha256': tree_hash(workspace)}
