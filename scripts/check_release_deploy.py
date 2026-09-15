#!/usr/bin/env python3
"""Static company-deployment contract; no live credentials or mail required."""
import json
from pathlib import Path
import sys

# Input is docker compose config --format json, generated with disposable env.
cfg=json.load(sys.stdin)
services=cfg['services']
web=services['web']
assert web['build']['args']['INTERNAL_API_URL']=='http://tabmail-api:8080'
for name in ('tabmail-api','tabmail-smtp','tabmail-worker','tabmail-retention'):
 env=services[name]['environment']
 assert str(env['TABMAIL_OPEN_REGISTRATION']).lower()=='false',name
 assert str(env['TABMAIL_OUTBOUND_ENABLED']).lower()=='true',name
 assert env['TABMAIL_OUTBOUND_MODE']=='relay',name
 assert env['TABMAIL_OUTBOUND_RELAY_HOST']=='relay.example.invalid',name
 for suffix in ('PORT','USER','PASS','TLS'):
  assert 'TABMAIL_OUTBOUND_RELAY_'+suffix in env,name
 assert str(env['TABMAIL_INGEST_DURABLE']).lower()=='true',name
assert services['redis']['healthcheck']['test']==['CMD-SHELL','redis-cli ping']
assert services['redis']['environment']['REDISCLI_AUTH']
print('PASS: company defaults, relay propagation, standalone target and authenticated Redis healthcheck')
