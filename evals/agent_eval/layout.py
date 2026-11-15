"""Current trial paths and append-only verification outputs."""
from pathlib import Path
import re

from .storage import read_json, validate_output_directory


class TrialLayout:
    def __init__(self, root):
        self.root = Path(root)

    @classmethod
    def read(cls, root):
        if read_json(Path(root) / 'receipt.json').get('layout_version') != 2:
            raise ValueError('unsupported trial layout; old reports can be viewed directly')
        return cls(root)

    def trace(self, name):
        if name not in {'protocol.jsonl', 'calls.jsonl', 'events.jsonl', 'stderr.log', 'observed.json', 'tasks.json', 'preparation.log'}:
            raise ValueError('unknown trace file')
        return self.root / 'trace' / name

    @property
    def verification(self):
        return self.root / 'verification/v1/results'

    def prepare(self):
        self.trace('protocol.jsonl').parent.mkdir(parents=True, exist_ok=True)


def new_verification(trial, explicit=None):
    """Reserve a fresh output directory. Never replace prior derived evidence."""
    if explicit is not None:
        directory = Path(explicit).resolve()
        original = Path(trial).resolve()
        if directory == original or original.is_relative_to(directory):
            raise ValueError('derived output overlaps original trial')
        if directory.is_relative_to(original):
            parts = directory.relative_to(original).parts
            if len(parts) != 2 or parts[0] != 'verification' or not re.fullmatch(r'v[1-9][0-9]*', parts[1]):
                raise ValueError('derived output must use a fresh verification/vN directory')
        validate_output_directory(Path(__file__).resolve().parents[2], directory)
        directory.mkdir(parents=True, exist_ok=False)
        return directory
    trial = Path(trial)
    parent = trial / 'verification'
    validate_output_directory(Path(__file__).resolve().parents[2], parent)
    if parent.is_symlink():
        raise ValueError('derived directory must not be a symbolic link')
    parent.mkdir(parents=True, exist_ok=True)
    versions = [int(p.name[1:]) for p in parent.iterdir() if re.fullmatch(r'v[1-9][0-9]*', p.name)]
    version = max([0, *versions]) + 1
    while True:
        output = parent / f'v{version}'
        try:
            output.mkdir()
            return output
        except FileExistsError:
            version += 1
