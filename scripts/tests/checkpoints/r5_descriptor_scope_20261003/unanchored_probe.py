import ast
import json
import subprocess
import unittest
from unittest import mock
import test_r5_excluded_descriptor_metadata as target
base = subprocess.check_output(['git', 'show', 'aeed47f0e1998d9925acb680992b051463821d69:scripts/tests/test_r5_excluded_descriptor_metadata.py'], text=True)
wrapper = next(ast.literal_eval(node.value) for node in ast.parse(base).body
               if isinstance(node, ast.Assign) and any(isinstance(n, ast.Name) and n.id == 'WRAPPER' for n in node.targets))
wrapper = wrapper.replace('    return result', '''            if fault == "regular":
                new = original(path, *args, **kwargs)
                print("UNANCHORED_REPLACEMENT", json.dumps({
                    "old": [result.st_dev, result.st_ino, result.st_mode],
                    "new": [new.st_dev, new.st_ino, new.st_mode],
                    "phase": calls, "old_nlink": result.st_nlink,
                    "new_nlink": new.st_nlink}), file=sys.stderr)
    return result''')
case = target.DescriptorMetadataTests('test_bounded_replacements_api_and_cli')
case.setUp()
original_run = subprocess.run
try:
    receipt = case.capture()
    import hashlib
    path = case.root.parent / 'receipt.json'
    path.write_bytes(target.inventory.canonical(receipt))
    pin = hashlib.sha256(path.read_bytes()).hexdigest()
    for _, _, fork, _ in target.oracle.FORKS:
        directory = case.root / fork / 'build'; directory.mkdir()
        artifact = directory / 'switch'
        for action in ['capture', 'validate']:
            args = [action, '--root', str(case.root), '--purpose', 'protocol', '--policy', target.oracle.POLICY]
            args += ['--build-context', json.dumps(target.oracle.CONTEXT)] if action == 'capture' else ['--receipt', str(path), '--receipt-sha256', pin]
            for entry in ['cli', 'api']:
                artifact.touch()
                observed_wrapper = wrapper
                if entry == 'api':
                    # Original public API dispatch, as in the baseline test.
                    observed_wrapper = observed_wrapper.replace('status = module.main(sys.argv[5:])', '''try:
    if sys.argv[5] == "capture":
        module.capture_current_source(target.parents[3], purpose="protocol", policy=module.CURRENT_POLICY, build_context=json.loads(sys.argv[-1]))
    else:
        module.validate_current_source(json.loads(pathlib.Path(sys.argv[-3]).read_text()), target.parents[3], purpose="protocol", policy=module.CURRENT_POLICY)
    status = 0
except (ValueError, OSError) as error:
    print(json.dumps({"error": str(error)}))
    status = 1''')
                result = original_run([target.sys.executable, '-B', '-c', observed_wrapper, str(target.oracle.SOURCE), str(artifact), 'regular', '1', *args], capture_output=True, text=True, timeout=10)
                print(json.dumps(dict(fork=fork, action=action, entry=entry, returncode=result.returncode,
                                     observations=result.stderr.splitlines())), flush=True)
                artifact.unlink()
        directory.rmdir()
finally:
    case.doCleanups()
