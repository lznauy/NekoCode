"""Mechanical checks only. Visual meaning is reviewed from saved screenshots."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import xml.etree.ElementTree as ET

MECHANICAL = ['svg_document', 'self_contained', 'vector_only', 'rendered']
VISUAL = ['pelican_identity', 'bicycle_structure', 'riding_pose', 'visual_quality']


def inspect_svg(path):
    checks = []
    try:
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 2 * 1024 * 1024:
            raise ValueError('SVG must be a regular file no larger than 2 MiB')
        text = path.read_text(encoding='utf-8')
        if re.search(r'<!\s*(DOCTYPE|ENTITY)|<\?xml-stylesheet', text, re.I):
            raise ValueError('DTD/entity declarations are not supported')
        root = ET.fromstring(text)
        if root.tag != '{http://www.w3.org/2000/svg}svg':
            raise ValueError('SVG namespace/root missing')
        viewbox = [float(v) for v in re.split(r'[\s,]+', root.get('viewBox', '').strip())]
        import math
        if len(viewbox) != 4 or not all(math.isfinite(v) for v in viewbox) or min(viewbox[2:]) <= 0:
            raise ValueError('viewBox must contain four finite numbers and positive dimensions')
        checks.append(dict(id='svg_document', status='passed', evidence='XML namespace and viewBox parsed'))
    except (OSError, UnicodeError, ValueError, ET.ParseError) as exc:
        return [dict(id='svg_document', status='failed', evidence=str(exc))] + [
            dict(id=i, status='not_run', evidence='Invalid/missing SVG') for i in MECHANICAL[1:]]
    violations = []
    vectors = True
    for node in root.iter():
        tag = node.tag.rsplit('}', 1)[-1].lower()
        if tag in {'script', 'foreignobject', 'animate', 'animatetransform', 'animatemotion', 'set'}:
            violations.append('script, foreign content or animation element: ' + tag)
        if tag in {'image', 'feimage'}:
            vectors = False
        for key, value in node.attrib.items():
            name = key.rsplit('}', 1)[-1].lower()
            if name.startswith('on') or (name in {'href', 'src', 'base'} and not value.strip().startswith('#')):
                violations.append('event handler or non-fragment reference: ' + name)
    # Inspect the XML-decoded values the renderer sees, not just source text.
    # CSS escapes are conservatively unsupported: this is a task contract,
    # not a general-purpose SVG security sanitizer.
    css_values = [value for node in root.iter() for value in node.attrib.values()]
    css_values += [''.join(node.itertext()) for node in root.iter()
                   if node.tag.rsplit('}', 1)[-1].lower() == 'style']
    for css in css_values:
        if '@import' in css.lower() or '\\' in css:
            violations.append('CSS import/escape unsupported')
        for value in re.findall(r'url\s*\((.*?)\)', css, flags=re.I | re.S):
            if not value.strip().strip('\'"').startswith('#'):
                violations.append('non-fragment CSS url')
    for node in root.iter():
        for key, value in node.attrib.items():
            if key.startswith('{http://www.w3.org/XML/1998/namespace}') and key.endswith('base'):
                violations.append('xml:base unsupported')
    checks.append(dict(id='self_contained', status='failed' if violations else 'passed', evidence='; '.join(violations) or 'No active/external references found'))
    checks.append(dict(id='vector_only', status='passed' if vectors else 'failed', evidence='No image/feImage elements' if vectors else 'Raster/image element present'))
    return checks


def main():
    workspace, output = map(Path, sys.argv[1:3])
    checks = inspect_svg(workspace / 'pelican.svg')
    error = None
    if len(checks) == 3 and all(c['status'] == 'passed' for c in checks):
        try:
            chrome = os.environ.get('CHROME_BIN') or shutil.which('google-chrome') or shutil.which('chromium')
            if not chrome:
                raise RuntimeError('Chrome unavailable; configure CHROME_BIN')
            subprocess.run(['node', str(Path(__file__).with_name('browser.cjs')), str(workspace / 'pelican.svg'), str(output)],
                           env=dict(os.environ, CHROME_BIN=chrome), check=True, timeout=45)
            checks.append(json.loads((output / 'render.json').read_text()))
        except (OSError, ValueError, subprocess.SubprocessError, RuntimeError) as exc:
            error = str(exc)
            checks.append(dict(id='rendered', status='not_run', evidence=error))
    elif len(checks) == 3:
        checks.append(dict(id='rendered', status='not_run', evidence='Source contract failed; rendering skipped'))
    checks += [dict(id=i, status='not_run', evidence='Requires screenshot review; automatic success covers mechanical delivery only. See review.md.') for i in VISUAL]
    (output / 'result.json').write_text(json.dumps(dict(schema_version=1, checks=checks, error=error)))


if __name__ == '__main__':
    main()
