"""Synthetic design model only; no repository imports or executable captures.

This illustrates external digest authority. It does not implement the proposed
adapter, verify producer provenance, or qualify any repository consumer.
"""
import copy
import hashlib
import json


def sha(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def observation(value):
    return sha({k: v for k, v in value.items() if k != 'observation_sha256'})


def accepted(value, expected, version):
    return (type(version) is int and version == 3 and
            value['schema_version'] == version and
            observation(value) == expected == value['observation_sha256'])


def main():
    original = dict(schema_version=3, binding_sha256='synthetic-binding',
                    observation_envelope=[dict(role='hydration', raw_stdout_sha256='raw-a'),
                                          dict(role='selected', stderr_sha256='stderr-a')])
    original['observation_sha256'] = observation(original)
    controller_pin = observation(original)
    forged = copy.deepcopy(original)
    forged['observation_envelope'][0]['raw_stdout_sha256'] = 'forged'
    forged['observation_sha256'] = observation(forged)
    diagnostic = copy.deepcopy(original)
    diagnostic['observation_envelope'][1]['stderr_sha256'] = 'stderr-b'
    diagnostic['observation_sha256'] = observation(diagnostic)
    controls = {
        'correct_external_pin': accepted(original, controller_pin, 3),
        'missing_pin_rejected': not accepted(original, None, 3),
        'forged_self_resigned_rejected': not accepted(forged, controller_pin, 3),
        'diagnostic_change_requires_own_pin': not accepted(diagnostic, controller_pin, 3),
        'owned_new_observation_own_pin': accepted(diagnostic, observation(diagnostic), 3),
        'same_binding_distinct_observation': original['binding_sha256'] == diagnostic['binding_sha256'] and controller_pin != observation(diagnostic),
        'v2_option_rejects_v3': not accepted(original, controller_pin, 2),
        'v1_option_rejects_v3': not accepted(original, controller_pin, 1),
        'bool_option_rejected': not accepted(original, controller_pin, True),
        'unknown_option_rejected': not accepted(original, controller_pin, 4),
        'file_byte_pin_domain_distinct': hashlib.sha256((json.dumps(original, indent=2)+'\n').encode()).hexdigest() != controller_pin,
    }
    assert all(controls.values()), controls
    print(json.dumps(dict(kind='synthetic_design_checks_only', source_reviewed='412f875985854527f5e7d74040f18954c3a352bf',
                         inherited_tested_source='58a50ee7ff8cde0b434f8492426d2263cedf280d',
                         project_imports=0, producer_executions=0, formal_qualification=False,
                         controls=controls), indent=2))


if __name__ == '__main__':
    main()
