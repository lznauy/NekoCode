"""Mechanical review delivery only; accuracy needs external adjudication."""
import hashlib,json,os,sys
from pathlib import Path

def source_digest(root):
    values={}
    for path in sorted(root.rglob('*')):
        relative=path.relative_to(root)
        if any(part in {'.git','__pycache__'} for part in relative.parts):continue
        name=relative.as_posix()
        if path.is_symlink():values[name]={'symlink':os.readlink(path)}
        elif path.is_file():values[name]={'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'executable':bool(path.stat().st_mode & 0o111)}
    return hashlib.sha256(json.dumps(values,sort_keys=True,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()


def evaluate(workspace,answer,expected_sha256=None):
    try:value=json.loads(answer)
    except ValueError:value=None
    structured=isinstance(value,dict) and isinstance(value.get('findings'),list) and isinstance(value.get('reviewed_scopes'),list) and bool(value['reviewed_scopes']) and isinstance(value.get('limitations'),list)
    locations=structured
    if structured:
        for finding in value['findings']:
            if not isinstance(finding,dict):locations=False;continue
            name=finding.get('file');first=finding.get('start_line');last=finding.get('end_line')
            if not isinstance(name,str) or not name or Path(name).is_absolute() or '..' in Path(name).parts:locations=False;continue
            path=workspace/'repo'/name
            if path.is_symlink() or not path.resolve().is_relative_to((workspace/'repo').resolve()) or not path.is_file():locations=False;continue
            if type(first)!=int or type(last)!=int or not 1<=first<=last<=len(path.read_text(errors='replace').splitlines()):locations=False
            if finding.get('severity') not in ('high','medium','low') or finding.get('verification') not in ('executed','static','blocked'):structured=False
            if any(not isinstance(finding.get(k),str) or not finding[k].strip() for k in ['title','trigger','impact','evidence']):structured=False
            if not isinstance(finding.get('source_tasks'),list):structured=False
    if expected_sha256 is not None:
        unchanged=(workspace/'repo/go.mod').is_file() and (workspace/'review.patch').is_file() and source_digest(workspace)==expected_sha256
        unchanged_evidence='Source tree compared with the pre-execution digest; Git metadata and Python caches excluded'
    else:
        unchanged=None
        unchanged_evidence='No original source digest; run the repository case to verify its read-only contract'
    return {'schema_version':1,'checks':[
        {'id':'structured_report','status':'passed' if structured else 'failed','evidence':'JSON schema/coverage checked; text truth requires adjudication'},
        {'id':'source_unchanged','status':'passed' if unchanged else 'not_run' if unchanged is None else 'failed','evidence':unchanged_evidence},
        {'id':'valid_locations','status':'passed' if locations else 'failed','evidence':'Paths and line ranges checked, not semantic correctness'},
        {'id':'review_accuracy','status':'not_run','evidence':'Manual external adjudication required; automatic success is delivery only'}]}

if __name__=='__main__':
    workspace,output=map(Path,sys.argv[1:3])
    baseline=json.loads((output/'input.json').read_text())['sha256'] if (output/'input.json').is_file() else None
    result=evaluate(workspace,(output/'answer.txt').read_text(),baseline)
    (output/'result.json').write_text(json.dumps(result))
