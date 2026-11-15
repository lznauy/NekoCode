"""Metadata-only history navigation. Never reads agent traces or credentials."""
from pathlib import Path
from urllib.parse import quote

from .storage import read_json, write_json


def collect(root):
    root = Path(root)
    records, errors = [], []
    for path in sorted(root.glob('*/run.json'), reverse=True):
        run = path.parent
        try:
            if run.is_symlink():
                raise ValueError('symlink run directory unsupported')
            manifest = read_json(path)
            if not isinstance(manifest.get('trials'), list):
                raise ValueError('invalid trials list')
            for relative in manifest['trials']:
                if not isinstance(relative, str) or Path(relative).is_absolute():
                    raise ValueError('invalid trial path')
                trial = (run / relative).resolve()
                if not trial.is_relative_to(run.resolve()) or trial == run.resolve():
                    raise ValueError('trial path escapes run')
                receipt = read_json(trial/'receipt.json') if (trial/'receipt.json').exists() else manifest.get('trial_specs', {}).get(relative, {})
                outcome = read_json(trial/'outcome.json') if (trial/'outcome.json').exists() else None
                if not isinstance(receipt, dict) or (outcome is not None and not isinstance(outcome, dict)):
                    raise ValueError('invalid receipt/outcome object')
                if outcome is not None and not isinstance(outcome.get('cost', {}), dict):
                    raise ValueError('invalid cost object')
                records.append({'run_id': run.name, 'trial_id': Path(relative).name,
                                'label': manifest.get('label'), 'case_id': receipt.get('case_id'),
                                'repeat': receipt.get('repeat'), 'model': receipt.get('model_requested'),
                                'binary_sha256': receipt.get('binary_sha256'),
                                'layout_version': receipt.get('layout_version', 1),
                                'run_status': manifest.get('status', 'unknown'),
                                'success': outcome.get('success') if outcome else None,
                                'stop': outcome.get('stop_reason') if outcome else 'unfinished',
                                'elapsed_s': outcome.get('elapsed_s') if outcome else None,
                                'cost': outcome.get('cost', {}) if outcome else {},
                                'trial_path': (Path(run.name)/relative).as_posix(),
                                'report': (Path(run.name)/relative/'report.md').as_posix(),
                                'product': (Path(run.name)/relative/'product').as_posix() if outcome is None or outcome.get('product_retained') is not False else None})
        except (OSError, ValueError, KeyError, TypeError, AttributeError) as exc:
            errors.append({'run_id': run.name, 'error': str(exc)})
    return {'schema_version': 1, 'records': records, 'errors': errors}


def markdown(data):
    def cell(v):
        return str(v if v is not None else 'unknown').replace('\n', ' ').replace('\r', ' ').replace('|', '\\|')
    lines = ['# Evaluation history', '', 'Generated navigation only. Original runs are unchanged.', '',
             '| Run / label | Case | Model / binary | Delivery / stop | Seconds | Model calls | Tokens / known subtotal | Files |',
             '|---|---|---|---|---:|---:|---|---|']
    for r in data['records']:
        cost = r['cost']
        tokens = cost.get('total_tokens')
        if tokens is None and cost.get('observed_total_tokens') is not None:
            tokens = f"unknown / ≥{cost['observed_total_tokens']}"
        product = f" · [product]({quote(r['product'], safe='/')})" if r['product'] else ''
        lines.append(f"| {cell(r['run_id'])} / {cell(r['label'])} | {cell(r['case_id'])} | {cell(r['model'])} / {cell(str(r['binary_sha256'])[:12] if r['binary_sha256'] else None)} | {cell(r['success'])} / {cell(r['stop'])} | {cell(r['elapsed_s'])} | {cell(cost.get('model_calls'))} | {cell(tokens)} | [report]({quote(r['report'], safe='/')}){product} |")
    if data['errors']:
        lines += ['', '## Unreadable records', '']
        lines += [f"- {cell(e['run_id'])}: {cell(e['error'])}" for e in data['errors']]
    return '\n'.join(lines)+'\n'


def refresh(root):
    data = collect(root)
    for name in ['index.json', 'index.md', 'index.json.tmp']:
        if (Path(root)/name).is_symlink():
            raise ValueError('history index must not be a symbolic link')
    write_json(Path(root)/'index.json', data)
    (Path(root)/'index.md').write_text(markdown(data))
    return data
