"""Frozen revision-1 through revision-12 reviews and current revision-13 facts."""
import ast
from contextlib import contextmanager, ExitStack
import copy
from collections import Counter
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import check_r5_transactions as tx
import r5_go_environment
import check_r5_compatibility as gate

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261004'
REVISION3_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261007'
REVISION4_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007'
REVISION5_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION5-20261007'
REVISION6_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION6-20261008'
REVISION7_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION7-20261008'
REVISION8_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION8-20261008'
REVISION9_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION9-20261008'
REVISION10_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION10-20261008'
CURRENT_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION13-20261008'
REVISION10_COMMIT = '20c39ab3ebaee46e5ac51e25d90a455166c9a31d'
REVISION10_SOURCE_COMMIT = '703a572864296120fff3efe0880c560ad9c74d57'
REVISION10_SOURCE_TREE = '40e1d2d16258e0e2bc3afea29553dbb148920e4e'
REVISION9_COMMIT = 'f2215611158d33ff9caed468bf6314e32dcf5046'
REVISION9_SOURCE_COMMIT = '25b0b294f4fa0bb93b0c82304a570eae43a0923a'
REVISION9_SOURCE_TREE = 'db7c422fcdd63405706a2f2cb7631a5380f87fd6'
REVISION8_COMMIT = '1d856bd8a552c30dfb48b4902858edad7e53aaf5'
REVISION8_SOURCE_COMMIT = 'e0cd175996ca4ee314d7d8b8cf836023b680346c'
REVISION8_SOURCE_TREE = '5307be3cf057104d1bf1529e38235bbaf0c2bcdf'
SOURCE_COMMIT = '9b73b13f376f7e8d589a06fc7078dae4c342d5d5'
REVISION7_COMMIT = 'f77c31e2da38bb926dfe6fa134eabad652e94f8c'
REVISION7_SOURCE_COMMIT = '9b12c93cb03285298267e27893f74aebe742a8a2'
SOURCE_TREE = '8e3f458f8cec51c031a06fd280151116164244ec'
REVISION7_SOURCE_TREE = '87c87a0db72ac444050b43fbe67d906f45c4a2ac'
REVISION6_COMMIT = 'c3e1419e6245baf0190291ea868787cb2b0177ca'
REVISION6_SOURCE_COMMIT = 'f413a9138d305cf154ed2cecaddcf9b9a2397666'
REVISION6_SOURCE_TREE = '37adfe5efb4ae49274a6d52a5d769bcb5b116cf0'
REVISION5_SOURCE_COMMIT = '79738c17d185f1cd5cd1c8d550a9f15a5501f31c'
REVISION5_COMMIT = '697b12d70b84658f5429e9a88ad2efde94e2e443'
REVISION4_SOURCE_COMMIT = 'e32564304c0c84aa80b1184e72129fb0316d989d'
REVISION4_COMMIT = '6b6163dc8941aa36bb9fa2b75f40c101b8f662fe'
REVISION3_SOURCE_COMMIT = '7b7dbfeaad5c87e875bf19e5a9e213867fda2db3'
REVISION3_COMMIT = '4065c4909c8f21a401a9a1af6370fa3f72670b99'
REVISION2_COMMIT = '4540fb91ba742443dcf0a872b80261e58af7b3ab'
# These identities are independent of the current catalogs and review packet.
# A missing historical Git object is an error; never fetch, skip, or fall back
# to the current same-named file when verifying a historical review.
REVISION2_SNAPSHOTS = {
    'transaction': {
        'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
        'blob': '3e78588a301512e02d718666c54f9ea4d13640e0',
        'sha256': '9fe6a4d73935b8e51e064c9afe16279187d0516f2959189dabf58bf39e9a6bef',
    },
    'compatibility': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
        'blob': '4bddcd23e6d799c05a7fade19a3ceb6456576b3d',
        'sha256': '3c03f670576ecae4d260b6995c7230dde8a598288157940cd08c2921b78a5f06',
    },
    'clients': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
        'blob': '2d2990ca3f1672af09fedb50f6f826aa15bcea0b',
        'sha256': 'f4a06b6911bfa888d4720235759b0bd59ee1314eddfdc9fe85524eca7e9c2c4f',
    },
    'client_routes': {
        'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
        'blob': '1dead7eb284dc8b2de7d56b58db40636785b4ddb',
        'sha256': '3fdaa56861ba4e524ddcdbf1d69b2b8513e89f2567500d0410effdf73ab1a617',
    },
}


REVISION3_SNAPSHOTS = {
    'transaction': {
        'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
        'blob': '2dbd71b6b9f2ccd6eb1bf141f439b6ff83a1f26e',
        'sha256': '3a49928f35308951340025741ba0c308e8fecf20fc7792bc5b45115175f3f009',
    },
    'compatibility': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
        'blob': '6b14b9ec4e8aac5caac5d74f47847b16e25e9e5e',
        'sha256': '0817aa5c77142849401dafe8e5acf3194feedc489539442a3cdcf9686cfd7f0d',
    },
    'clients': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
        'blob': 'f9f82391f73b3e8a5680c015a860bf880a429d2d',
        'sha256': '842ade963ab45a9215123929ed1db529da5d94c4a8c2462f612df9c615e3399c',
    },
    'client_routes': {
        'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
        'blob': '1d375226040c4edb3b4571eaddf1396f875643b1',
        'sha256': '8e00ecf63e11b46062add84f062ad7f7d780949ae842127e5aed055db05fb6de',
    },
}


REVISION4_SNAPSHOTS = {
    'transaction': {
        'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
        'blob': 'bd50c385e9c6c385a650f68ce5c5857ee69986ea',
        'sha256': '5fc5fb70255b5e6986b9aab38157fbb915b6012377d76d5b2a1956fc2907de2c',
    },
    'compatibility': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
        'blob': '23a227e3eae3aacc44d2c2be97c35bfa55bcc8a6',
        'sha256': '56452854110d0c33b5d2ae9eaf1abe43a7f77681a59307fc2470796dbdc18f56',
    },
    'clients': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
        'blob': '9989cd7fe881f23eb2fe5b08e1a390e3128033e3',
        'sha256': '83eeb4a43fa3a6c762353c1597c00e998ca64bf68138a7eb165984095b110e3d',
    },
    'client_routes': {
        'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
        'blob': '6a9cac1e365ce2664c6cddb170009d52fec2184c',
        'sha256': '0877601e0a825ba28f2e912bc057cf92e7d607ca568f5d1b29621e2798dc5139',
    },
}


REVISION5_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': 'f34b037f26dc23464fe595a85b310bbed2f4a409',
                 'sha256': '0ecb372399d4878fa46d454c88fc48e8cb1e40a06541beaef6309517c8cb6866'},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': '46175d14abb95668baaed6143d6e038c88e62ba1',
                   'sha256': '73b30e5733dccba31b1330eec0c52b66a96fb9f3f2dfb19fd3069a2ba32ddf80'},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': 'c1dc0d0acc96e9f6375bf9a82c9a3a6d3a1d8ae1',
             'sha256': '76d03e2c33420a36cf49b0e740656ec83e672598028b9b0257a2de8d171acec7'},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': '63a497f7cdcdb4002d0139c8f68681c0dfb32781',
                   'sha256': '39e65cca57bcddfe9a8b67733cd240a2db0c86f372ec491161ee7ec94dd5b72d'}}

REVISION6_EXPECTED = {'caller_ids': ['internal/store/postgres/audit.go:*PgStore:InsertAudit',
                'internal/store/postgres/company_mail.go:*PgStore:FinishMailAttachment',
                'internal/store/postgres/company_mail.go:*PgStore:GetMailDraft',
                'internal/store/postgres/company_mail.go:*PgStore:GetWorkAttachment',
                'internal/store/postgres/company_mail.go:*PgStore:ReserveMailAttachment',
                'internal/store/postgres/company_mail.go:*capturedRestoreDeadline:check',
                'internal/store/postgres/company_members.go:*PgStore:GetWorkMailbox',
                'internal/store/postgres/domain_delete.go:*PgStore:DeleteZone',
                'internal/store/postgres/mail_content.go:*PgStore:GetParsedMessage',
                'internal/store/postgres/mail_content.go:*PgStore:SaveParsedMessage',
                'internal/store/postgres/mailboxes.go:*PgStore:GetMailbox',
                'internal/store/postgres/mailboxes.go:*pgTenantView:GetMailbox',
                'internal/store/postgres/outbound.go:*PgStore:DeleteSuppressionAuthorized',
                'internal/store/postgres/outbound.go:*PgStore:IsSuppressed',
                'internal/store/postgres/outbound.go:*PgStore:ListSuppressions',
                'internal/store/postgres/outbound_content.go:*PgStore:CanReadOutboundContent',
                'internal/store/postgres/outbound_retry.go:*PgStore:RequeueOutboundJobAuthorized',
                'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetZone',
                'internal/store/postgres/outbound_retry_reader.go:retryTenantView:GetMailbox',
                'internal/store/postgres/postgres.go:*PgStore:Close',
                'internal/store/postgres/postgres.go::New',
                'internal/store/postgres/queue.go:*PgStore:CountDeadWebhookDeliveries',
                'internal/store/postgres/queue.go:*PgStore:CreateOutboxEvent',
                'internal/store/postgres/queue.go:*PgStore:CreateWebhookDeliveries',
                'internal/store/postgres/queue.go:*PgStore:ListDeadWebhookDeliveries',
                'internal/store/postgres/send_identities.go:*PgStore:CreateSendIdentity',
                'internal/store/postgres/send_identities.go:*PgStore:UpdateSendIdentitiesVerifiedByZone',
                'internal/store/postgres/submissions.go:*PgStore:GetSubmissionAttachment',
                'internal/store/postgres/submissions.go:*PgStore:GetSubmissionContent',
                'internal/store/postgres/submissions.go:*PgStore:ListSubmissionAttachments',
                'internal/store/postgres/tenants.go:*PgStore:EffectiveConfig',
                'internal/store/postgres/webhook_endpoints.go:*PgStore:ListWebhookEndpoints',
                'internal/store/postgres/zones.go:*PgStore:CountZones',
                'internal/store/postgres/zones.go:*PgStore:CreateZone',
                'internal/store/postgres/zones.go:*PgStore:GetZone',
                'internal/store/postgres/zones.go:*PgStore:GetZoneByDomain',
                'internal/store/postgres/zones.go:*PgStore:ListAllZones',
                'internal/store/postgres/zones.go:*PgStore:ListZones',
                'internal/store/postgres/zones.go:*PgStore:ListZonesByVisibilities',
                'internal/store/postgres/zones.go:*PgStore:ListZonesScoped',
                'internal/store/postgres/zones.go:*PgStore:UpdateZone'],
 'caller_locations': [{'id': 'internal/store/postgres/audit.go:*PgStore:InsertAudit',
                       'caller_id': 'internal/app/domains/service.go:*Service:CreateZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 'app.InsertAudit',
                       'before_line': 273,
                       'after_line': 312},
                      {'id': 'internal/store/postgres/audit.go:*PgStore:InsertAudit',
                       'caller_id': 'internal/app/domains/service.go:*Service:TriggerVerify',
                       'source': 'internal/app/domains/service.go',
                       'expression': 'app.InsertAudit',
                       'before_line': 345,
                       'after_line': 390},
                      {'id': 'internal/store/postgres/company_mail.go:*PgStore:FinishMailAttachment',
                       'caller_id': 'internal/app/companymail/service.go:*Service:UploadAttachment',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.FinishMailAttachment',
                       'before_line': 287,
                       'after_line': 289},
                      {'id': 'internal/store/postgres/company_mail.go:*PgStore:GetMailDraft',
                       'caller_id': 'internal/app/submissions/service.go:*Service:SubmitDraft',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.repo.GetMailDraft',
                       'before_line': 151,
                       'after_line': 152},
                      {'id': 'internal/store/postgres/company_mail.go:*PgStore:GetWorkAttachment',
                       'caller_id': 'internal/app/companymail/service.go:*Service:Attachment',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.GetWorkAttachment',
                       'before_line': 317,
                       'after_line': 319},
                      {'id': 'internal/store/postgres/company_mail.go:*PgStore:GetWorkAttachment',
                       'caller_id': 'internal/app/companymail/service.go:*Service:Attachment',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.GetWorkAttachment',
                       'before_line': 332,
                       'after_line': 334},
                      {'id': 'internal/store/postgres/company_mail.go:*PgStore:ReserveMailAttachment',
                       'caller_id': 'internal/app/companymail/service.go:*Service:UploadAttachment',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.ReserveMailAttachment',
                       'before_line': 280,
                       'after_line': 282},
                      {'id': 'internal/store/postgres/company_mail.go:*capturedRestoreDeadline:check',
                       'caller_id': 'internal/app/companymail/read_boundary.go:*authorizedSource:Read',
                       'source': 'internal/app/companymail/read_boundary.go',
                       'expression': 'r.check',
                       'before_line': 51,
                       'after_line': 75},
                      {'id': 'internal/store/postgres/company_members.go:*PgStore:GetWorkMailbox',
                       'caller_id': 'internal/app/companymail/service.go:*Service:sender',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.GetWorkMailbox',
                       'before_line': 240,
                       'after_line': 242},
                      {'id': 'internal/store/postgres/domain_delete.go:*PgStore:DeleteZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:DeleteZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.DeleteZone',
                       'before_line': 295,
                       'after_line': 334},
                      {'id': 'internal/store/postgres/mail_content.go:*PgStore:GetParsedMessage',
                       'caller_id': 'internal/app/companymail/service.go:*Service:document',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 'cache.GetParsedMessage',
                       'before_line': 136,
                       'after_line': 138},
                      {'id': 'internal/store/postgres/mail_content.go:*PgStore:SaveParsedMessage',
                       'caller_id': 'internal/app/companymail/service.go:*Service:document',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 'cache.SaveParsedMessage',
                       'before_line': 157,
                       'after_line': 159},
                      {'id': 'internal/store/postgres/mailboxes.go:*PgStore:GetMailbox',
                       'caller_id': 'internal/app/submissions/service.go:*Service:SubmitDraft',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.store.GetMailbox',
                       'before_line': 178,
                       'after_line': 179},
                      {'id': 'internal/store/postgres/mailboxes.go:*pgTenantView:GetMailbox',
                       'caller_id': 'internal/app/submissions/service.go:*Service:SubmitDraft',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.store.GetMailbox',
                       'before_line': 178,
                       'after_line': 179},
                      {'id': 'internal/store/postgres/outbound.go:*PgStore:DeleteSuppressionAuthorized',
                       'caller_id': 'internal/api/handlers/outbound.go:*OutboundHandler:DeleteSuppression',
                       'source': 'internal/api/handlers/outbound.go',
                       'expression': 'h.store.DeleteSuppressionAuthorized',
                       'before_line': 247,
                       'after_line': 249},
                      {'id': 'internal/store/postgres/outbound.go:*PgStore:IsSuppressed',
                       'caller_id': 'internal/app/submissions/service.go:*Service:SubmitAuthorized',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.store.IsSuppressed',
                       'before_line': 325,
                       'after_line': 338},
                      {'id': 'internal/store/postgres/outbound.go:*PgStore:ListSuppressions',
                       'caller_id': 'internal/api/handlers/outbound.go:*OutboundHandler:ListSuppressions',
                       'source': 'internal/api/handlers/outbound.go',
                       'expression': 'h.store.ListSuppressions',
                       'before_line': 195,
                       'after_line': 194},
                      {'id': 'internal/store/postgres/outbound_content.go:*PgStore:CanReadOutboundContent',
                       'caller_id': 'internal/app/submissions/service.go:*Service:ContentAllowed',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.store.CanReadOutboundContent',
                       'before_line': 413,
                       'after_line': 425},
                      {'id': 'internal/store/postgres/outbound_retry.go:*PgStore:RequeueOutboundJobAuthorized',
                       'caller_id': 'internal/app/submissions/service.go:*Service:RetryOutboundJob',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.store.RequeueOutboundJobAuthorized',
                       'before_line': 427,
                       'after_line': 439},
                      {'id': 'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:ownedZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.GetZone',
                       'before_line': 392,
                       'after_line': 443},
                      {'id': 'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:validateZoneAncestry',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.GetZone',
                       'before_line': 505,
                       'after_line': 566},
                      {'id': 'internal/store/postgres/outbound_retry_reader.go:retryTenantView:GetMailbox',
                       'caller_id': 'internal/app/submissions/service.go:*Service:SubmitDraft',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.store.GetMailbox',
                       'before_line': 178,
                       'after_line': 179},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/api/handlers/respond.go::decodeBody',
                       'source': 'internal/api/handlers/respond.go',
                       'expression': 'r.Body.Close',
                       'before_line': 100,
                       'after_line': 106},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:dispatch',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'resp.Body.Close',
                       'before_line': 423,
                       'after_line': 435},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/testpg/fixture.go::NewPostgres',
                       'source': 'internal/testpg/fixture.go',
                       'expression': 'st.Close',
                       'before_line': 59,
                       'after_line': 73},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/companymail/service.go:*Service:UploadAttachment',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 'errors.New',
                       'before_line': 268,
                       'after_line': 270},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/companymail/service.go:*Service:open',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 'errors.New',
                       'before_line': 119,
                       'after_line': 121},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/companymail/service.go:*Service:open',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 'errors.New',
                       'before_line': 126,
                       'after_line': 125},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/companymail/service.go:*Service:verifiedFile',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 'errors.New',
                       'before_line': 300,
                       'after_line': 302},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/companymail/service.go:*Service:verifiedFile',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 'errors.New',
                       'before_line': 311,
                       'after_line': 313},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/domains/service.go:*Service:CreateZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 'uuid.New',
                       'before_line': 242,
                       'after_line': 281},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/domains/service.go::NewService',
                       'source': 'internal/app/domains/service.go',
                       'expression': 'authz.New',
                       'before_line': 104,
                       'after_line': 130},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/app/submissions/service.go::NewService',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 'authz.New',
                       'before_line': 124,
                       'after_line': 125},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:Publish',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'uuid.New',
                       'before_line': 175,
                       'after_line': 174},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:Publish',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'uuid.New',
                       'before_line': 189,
                       'after_line': 192},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:dispatch',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'errors.New',
                       'before_line': 429,
                       'after_line': 441},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/hooks/dispatcher.go::sign',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'hmac.New',
                       'before_line': 447,
                       'after_line': 459},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/testpg/fixture.go::NewPostgres',
                       'source': 'internal/testpg/fixture.go',
                       'expression': 'pgxpool.New',
                       'before_line': 32,
                       'after_line': 46},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/testpg/fixture.go::NewPostgres',
                       'source': 'internal/testpg/fixture.go',
                       'expression': 'postgres.New',
                       'before_line': 55,
                       'after_line': 69},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/testpg/fixture.go::NewPostgres',
                       'source': 'internal/testpg/fixture.go',
                       'expression': 'pgxpool.New',
                       'before_line': 60,
                       'after_line': 74},
                      {'id': 'internal/store/postgres/queue.go:*PgStore:CountDeadWebhookDeliveries',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:DeadLetterSize',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'd.store.CountDeadWebhookDeliveries',
                       'before_line': 219,
                       'after_line': 223},
                      {'id': 'internal/store/postgres/queue.go:*PgStore:CreateOutboxEvent',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:Publish',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'd.store.CreateOutboxEvent',
                       'before_line': 174,
                       'after_line': 177},
                      {'id': 'internal/store/postgres/queue.go:*PgStore:CreateWebhookDeliveries',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:processOutbox',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'd.store.CreateWebhookDeliveries',
                       'before_line': 300,
                       'after_line': 304},
                      {'id': 'internal/store/postgres/queue.go:*PgStore:ListDeadWebhookDeliveries',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:DeadLetters',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'd.store.ListDeadWebhookDeliveries',
                       'before_line': 200,
                       'after_line': 204},
                      {'id': 'internal/store/postgres/send_identities.go:*PgStore:CreateSendIdentity',
                       'caller_id': 'internal/app/domains/service.go:*Service:CreateZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.CreateSendIdentity',
                       'before_line': 270,
                       'after_line': 309},
                      {'id': 'internal/store/postgres/send_identities.go:*PgStore:UpdateSendIdentitiesVerifiedByZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:TriggerVerify',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.UpdateSendIdentitiesVerifiedByZone',
                       'before_line': 342,
                       'after_line': 387},
                      {'id': 'internal/store/postgres/submissions.go:*PgStore:GetSubmissionAttachment',
                       'caller_id': 'internal/app/companymail/service.go:*Service:SubmissionAttachment',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.GetSubmissionAttachment',
                       'before_line': 351,
                       'after_line': 353},
                      {'id': 'internal/store/postgres/submissions.go:*PgStore:GetSubmissionAttachment',
                       'caller_id': 'internal/app/companymail/service.go:*Service:SubmissionAttachment',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.GetSubmissionAttachment',
                       'before_line': 367,
                       'after_line': 369},
                      {'id': 'internal/store/postgres/submissions.go:*PgStore:GetSubmissionContent',
                       'caller_id': 'internal/app/companymail/service.go:*Service:SubmissionContent',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.GetSubmissionContent',
                       'before_line': 343,
                       'after_line': 345},
                      {'id': 'internal/store/postgres/submissions.go:*PgStore:ListSubmissionAttachments',
                       'caller_id': 'internal/app/companymail/service.go:*Service:SubmissionAttachments',
                       'source': 'internal/app/companymail/service.go',
                       'expression': 's.repo.ListSubmissionAttachments',
                       'before_line': 347,
                       'after_line': 349},
                      {'id': 'internal/store/postgres/tenants.go:*PgStore:EffectiveConfig',
                       'caller_id': 'internal/app/domains/service.go:*Service:CreateZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.EffectiveConfig',
                       'before_line': 226,
                       'after_line': 265},
                      {'id': 'internal/store/postgres/webhook_endpoints.go:*PgStore:ListWebhookEndpoints',
                       'caller_id': 'internal/hooks/dispatcher.go:*Dispatcher:processOutbox',
                       'source': 'internal/hooks/dispatcher.go',
                       'expression': 'd.store.ListWebhookEndpoints',
                       'before_line': 283,
                       'after_line': 287},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:CountZones',
                       'caller_id': 'internal/app/domains/service.go:*Service:CreateZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.CountZones',
                       'before_line': 230,
                       'after_line': 269},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:CreateZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:CreateZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.CreateZone',
                       'before_line': 254,
                       'after_line': 293},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:GetZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:ownedZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.GetZone',
                       'before_line': 392,
                       'after_line': 443},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:GetZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:validateZoneAncestry',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.GetZone',
                       'before_line': 505,
                       'after_line': 566},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:GetZoneByDomain',
                       'caller_id': 'internal/app/domains/service.go:*Service:findParentZone',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.GetZoneByDomain',
                       'before_line': 478,
                       'after_line': 539},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:GetZoneByDomain',
                       'caller_id': 'internal/app/submissions/service.go:*Service:SubmitAuthorized',
                       'source': 'internal/app/submissions/service.go',
                       'expression': 's.store.GetZoneByDomain',
                       'before_line': 240,
                       'after_line': 241},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:ListAllZones',
                       'caller_id': 'internal/app/domains/service.go:*Service:ListAllZones',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.ListAllZones',
                       'before_line': 157,
                       'after_line': 193},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:ListZones',
                       'caller_id': 'internal/app/domains/service.go::countOwnedZones',
                       'source': 'internal/app/domains/service.go',
                       'expression': 'st.ListZones',
                       'before_line': 570,
                       'after_line': 631},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:ListZonesByVisibilities',
                       'caller_id': 'internal/app/domains/service.go:*Service:ListOpenZones',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.ListZonesByVisibilities',
                       'before_line': 169,
                       'after_line': 205},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:ListZonesScoped',
                       'caller_id': 'internal/app/domains/service.go:*Service:ListZones',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.ListZonesScoped',
                       'before_line': 146,
                       'after_line': 182},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:UpdateZone',
                       'caller_id': 'internal/app/domains/service.go:*Service:TriggerVerify',
                       'source': 'internal/app/domains/service.go',
                       'expression': 's.store.UpdateZone',
                       'before_line': 336,
                       'after_line': 381}],
 'callers_added': [('internal/store/postgres/postgres.go:*PgStore:Close',
                    'internal/app/companymail/read_boundary.go:*authorizedSource:closeUnderlying',
                    'r.ReadCloser.Close'),
                   ('internal/store/postgres/postgres.go:*PgStore:Close',
                    'internal/app/companymail/read_boundary.go:*authorizedSource:finish',
                    'r.Close'),
                   ('internal/store/postgres/postgres.go:*PgStore:Close',
                    'internal/app/companymail/service.go:*Service:open',
                    'r.Close')],
 'callers_removed': [('internal/store/postgres/postgres.go:*PgStore:Close',
                      'internal/app/companymail/read_boundary.go:*authorizedSource:Close',
                      'r.ReadCloser.Close'),
                     ('internal/store/postgres/postgres.go:*PgStore:Close',
                      'internal/app/companymail/read_boundary.go:*authorizedSource:Read',
                      'r.Close')],
 'client_previous_indices': [0,
                             1,
                             2,
                             3,
                             4,
                             5,
                             6,
                             7,
                             8,
                             9,
                             10,
                             11,
                             12,
                             13,
                             14,
                             15,
                             16,
                             17,
                             18,
                             19,
                             20,
                             21,
                             22,
                             23,
                             24,
                             25,
                             27,
                             28,
                             29,
                             30,
                             31,
                             32,
                             33,
                             34,
                             35,
                             36,
                             37,
                             38,
                             39,
                             40,
                             41,
                             42,
                             43,
                             44,
                             45,
                             46,
                             47,
                             48,
                             49,
                             50,
                             51,
                             52,
                             53,
                             54,
                             55,
                             56,
                             57,
                             58,
                             59,
                             60,
                             61,
                             62,
                             63,
                             64,
                             65,
                             66,
                             67,
                             68,
                             69,
                             70,
                             71,
                             72,
                             73,
                             74,
                             75,
                             76,
                             77,
                             78,
                             79,
                             80,
                             81,
                             82,
                             83,
                             84,
                             85,
                             86,
                             87,
                             88,
                             89,
                             90,
                             91,
                             92,
                             93,
                             94,
                             95,
                             96,
                             97,
                             98,
                             99,
                             100,
                             101,
                             102,
                             103,
                             104,
                             105,
                             106,
                             107,
                             108,
                             109,
                             110,
                             111,
                             112,
                             113,
                             114,
                             115,
                             116,
                             117,
                             118,
                             119,
                             120,
                             121,
                             122,
                             123,
                             124,
                             125,
                             126,
                             127,
                             128,
                             129,
                             130,
                             131,
                             132,
                             133],
 'client_branch_count': 133,
 'client_merges': [{'before_indices': [25, 26], 'after_index': 25}],
 'client_changed_indices': [(24, 24), (25, 25)],
 'client_overrides': {25: {'owner': 'changeGrant'}},
 'changed_routes': ['GET /api/v1/company/templates/{id}/grants', 'PUT /api/v1/company/templates/{id}/grants'],
 'closure_paths': ['docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
                   'internal/api/handlers/outbound.go',
                   'internal/api/handlers/respond.go',
                   'internal/models/models.go',
                   'web/app/(dashboard)/company/templates/page.tsx',
                   'web/components/company/templates/grants.tsx'],
 'source_paths': ['internal/api/handlers/outbound.go',
                  'internal/api/handlers/respond.go',
                  'internal/app/companymail/read_boundary.go',
                  'internal/app/companymail/service.go',
                  'internal/app/domains/service.go',
                  'internal/app/submissions/service.go',
                  'internal/hooks/dispatcher.go',
                  'internal/models/models.go',
                  'internal/testpg/fixture.go',
                  'web/app/(dashboard)/company/templates/page.tsx',
                  'web/components/company/templates/grants.tsx']}


REVISION6_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': '5b7b8a28f5b3817c0cd4049be4c9d350d7b4f1b1',
                 'sha256': 'e820435ec2357f2fa0d0509e4e0759124a85054c0f4fc35df887f683a3c4e715'},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': '4e134e4fbc8f35fff6be9d1da51511e2869fe971',
                   'sha256': '457fcd6c63ce7f0b80a3d38aa2d7153e243eaa06569898ff47932bc02ca0983c'},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': '570196772dae1d41af74e33c7ff7aa50e9316650',
             'sha256': 'c673d9b6d049283bcba582a5465448c94234d1388a4a55b43035fc8fcdebb679'},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': 'aff3a16242d3204c387684cd7ca641342205c55e',
                   'sha256': '56bbc1329b0433e215413542409c6486d08af406d45d0a1fc6a6dc73dc6c6d21'}}

REVISION7_EXPECTED = {'caller_ids': ['internal/store/postgres/ingress.go:*PgStore:CreateIngress',
                'internal/store/postgres/ingress.go:*PgStore:DeliverIngress',
                'internal/store/postgres/ingress.go:*PgStore:FailIngressTarget',
                'internal/store/postgres/ingress.go:*PgStore:HoldIngressTarget',
                'internal/store/postgres/ingress.go:*PgStore:ListIngressTargets',
                'internal/store/postgres/mail_content.go:*PgStore:ClaimMailIndexJobs',
                'internal/store/postgres/mail_content.go:*PgStore:CompleteMailIndexJob',
                'internal/store/postgres/mail_content.go:*PgStore:FailMailIndexJob',
                'internal/store/postgres/mailboxes.go:*PgStore:GetMailbox',
                'internal/store/postgres/mailboxes.go:*pgTenantView:GetMailbox',
                'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetZone',
                'internal/store/postgres/outbound_retry_reader.go:retryTenantView:GetMailbox',
                'internal/store/postgres/postgres.go:*PgStore:Close',
                'internal/store/postgres/postgres.go::New',
                'internal/store/postgres/zones.go:*PgStore:GetRoute',
                'internal/store/postgres/zones.go:*PgStore:GetZone'],
 'caller_locations': [{'id': 'internal/store/postgres/ingress.go:*PgStore:CreateIngress',
                       'caller_id': 'internal/ingest/recovery.go:*Service:acceptDurable',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 'ledger.CreateIngress',
                       'before_line': 61,
                       'after_line': 60},
                      {'id': 'internal/store/postgres/ingress.go:*PgStore:DeliverIngress',
                       'caller_id': 'internal/ingest/recovery.go:*Service:deliverTarget',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 'ledger.DeliverIngress',
                       'before_line': 183,
                       'after_line': 180},
                      {'id': 'internal/store/postgres/ingress.go:*PgStore:FailIngressTarget',
                       'caller_id': 'internal/ingest/recovery.go:*Service:processReceipt',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 'ledger.FailIngressTarget',
                       'before_line': 117,
                       'after_line': 114},
                      {'id': 'internal/store/postgres/ingress.go:*PgStore:HoldIngressTarget',
                       'caller_id': 'internal/ingest/recovery.go:*Service:processReceipt',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 'ledger.HoldIngressTarget',
                       'before_line': 115,
                       'after_line': 112},
                      {'id': 'internal/store/postgres/ingress.go:*PgStore:ListIngressTargets',
                       'caller_id': 'internal/ingest/recovery.go:*Service:processReceipt',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 'ledger.ListIngressTargets',
                       'before_line': 68,
                       'after_line': 70},
                      {'id': 'internal/store/postgres/mail_content.go:*PgStore:ClaimMailIndexJobs',
                       'caller_id': 'internal/app/mailindex/service.go:*Service:Batch',
                       'source': 'internal/app/mailindex/service.go',
                       'expression': 's.repo.ClaimMailIndexJobs',
                       'before_line': 23,
                       'after_line': 26},
                      {'id': 'internal/store/postgres/mail_content.go:*PgStore:CompleteMailIndexJob',
                       'caller_id': 'internal/app/mailindex/service.go:*Service:Batch',
                       'source': 'internal/app/mailindex/service.go',
                       'expression': 's.repo.CompleteMailIndexJob',
                       'before_line': 35,
                       'after_line': 50},
                      {'id': 'internal/store/postgres/mail_content.go:*PgStore:FailMailIndexJob',
                       'caller_id': 'internal/app/mailindex/service.go:*Service:Batch',
                       'source': 'internal/app/mailindex/service.go',
                       'expression': 's.repo.FailMailIndexJob',
                       'before_line': 30,
                       'after_line': 45},
                      {'id': 'internal/store/postgres/mailboxes.go:*PgStore:GetMailbox',
                       'caller_id': 'internal/ingest/recovery.go:*Service:deliverTarget',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 's.store.GetMailbox',
                       'before_line': 141,
                       'after_line': 138},
                      {'id': 'internal/store/postgres/mailboxes.go:*pgTenantView:GetMailbox',
                       'caller_id': 'internal/ingest/recovery.go:*Service:deliverTarget',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 's.store.GetMailbox',
                       'before_line': 141,
                       'after_line': 138},
                      {'id': 'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetZone',
                       'caller_id': 'internal/ingest/recovery.go:*Service:deliverTarget',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 's.store.GetZone',
                       'before_line': 148,
                       'after_line': 145},
                      {'id': 'internal/store/postgres/outbound_retry_reader.go:retryTenantView:GetMailbox',
                       'caller_id': 'internal/ingest/recovery.go:*Service:deliverTarget',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 's.store.GetMailbox',
                       'before_line': 141,
                       'after_line': 138},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/smtp/server.go:*Server:drain',
                       'source': 'internal/smtp/server.go',
                       'expression': 'ln.Close',
                       'before_line': 304,
                       'after_line': 305},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/smtp/server.go:*limitedConn:Close',
                       'source': 'internal/smtp/server.go',
                       'expression': 'c.Conn.Close',
                       'before_line': 261,
                       'after_line': 262},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/smtp/server.go:*limitedListener:Accept',
                       'source': 'internal/smtp/server.go',
                       'expression': 'conn.Close',
                       'before_line': 209,
                       'after_line': 210},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/smtp/server.go:*limitedListener:Close',
                       'source': 'internal/smtp/server.go',
                       'expression': 'l.Listener.Close',
                       'before_line': 235,
                       'after_line': 236},
                      {'id': 'internal/store/postgres/postgres.go:*PgStore:Close',
                       'caller_id': 'internal/smtp/server.go:*limitedListener:closeConnections',
                       'source': 'internal/smtp/server.go',
                       'expression': 'c.Close',
                       'before_line': 248,
                       'after_line': 249},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/ingest/recovery.go:*Service:acceptDurable',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 'uuid.New',
                       'before_line': 53,
                       'after_line': 52},
                      {'id': 'internal/store/postgres/postgres.go::New',
                       'caller_id': 'internal/ingest/recovery.go:*Service:processReceipt',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 'errors.New',
                       'before_line': 99,
                       'after_line': 96},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:GetRoute',
                       'caller_id': 'internal/ingest/recovery.go:*Service:deliverTarget',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 's.store.GetRoute',
                       'before_line': 166,
                       'after_line': 163},
                      {'id': 'internal/store/postgres/zones.go:*PgStore:GetZone',
                       'caller_id': 'internal/ingest/recovery.go:*Service:deliverTarget',
                       'source': 'internal/ingest/recovery.go',
                       'expression': 's.store.GetZone',
                       'before_line': 148,
                       'after_line': 145}],
 'callers_added': [('internal/store/postgres/postgres.go:*PgStore:Close',
                    'internal/ingest/recovery_read.go:*Service:readReceiptOriginal',
                    'input.Close'),
                   ('internal/store/postgres/postgres.go::New',
                    'internal/ingest/recovery_read.go:*Service:readReceiptOriginal',
                    'errors.New')],
 'callers_removed': [('internal/store/postgres/postgres.go:*PgStore:Close',
                      'internal/ingest/recovery.go:*Service:processReceipt',
                      'rc.Close')],
 'client_previous_indices': [0,
                             1,
                             2,
                             3,
                             4,
                             5,
                             6,
                             7,
                             8,
                             9,
                             10,
                             11,
                             12,
                             13,
                             14,
                             15,
                             16,
                             None,
                             17,
                             18,
                             19,
                             20,
                             21,
                             22,
                             23,
                             24,
                             25,
                             26,
                             27,
                             28,
                             29,
                             30,
                             31,
                             32,
                             33,
                             34,
                             35,
                             36,
                             37,
                             38,
                             39,
                             40,
                             41,
                             42,
                             43,
                             44,
                             45,
                             46,
                             47,
                             48,
                             49,
                             50,
                             51,
                             52,
                             53,
                             54,
                             55,
                             56,
                             57,
                             58,
                             59,
                             60,
                             61,
                             62,
                             63,
                             64,
                             65,
                             66,
                             67,
                             68,
                             69,
                             70,
                             71,
                             72,
                             73,
                             74,
                             75,
                             76,
                             77,
                             78,
                             79,
                             80,
                             81,
                             82,
                             83,
                             84,
                             85,
                             86,
                             87,
                             88,
                             89,
                             90,
                             91,
                             92,
                             93,
                             94,
                             95,
                             96,
                             97,
                             98,
                             99,
                             100,
                             101,
                             102,
                             103,
                             104,
                             105,
                             106,
                             107,
                             108,
                             109,
                             110,
                             111,
                             112,
                             113,
                             114,
                             115,
                             116,
                             117,
                             118,
                             119,
                             120,
                             121,
                             122,
                             123,
                             124,
                             125,
                             126,
                             127,
                             128,
                             129,
                             130,
                             131,
                             132],
 'client_branch_count': 134,
 'clients_added': [{'after_index': 17,
                    'row': {'source': 'web/components/company/grants.tsx',
                            'line': 49,
                            'branch': 0,
                            'callee': 'company',
                            'owner': 'snapshot',
                            'expression': '`${workPath(mailbox.mailbox.id)}/grants`',
                            'path': '/api/v1/company/mailboxes/{id}/grants',
                            'methods': ['GET'],
                            'forwarding': False},
                    'route_reference_index': 16,
                    'reason': 'An explicit current grants GET recovers from stale revision conflicts; it does '
                              'not replay the PUT mutation.'}],
 'client_changed_indices': [(6, 6), (7, 7), (17, 18), (18, 19), (19, 20)],
 'client_overrides': {6: {'owner': 'list'}},
 'changed_routes': ['GET /api/v1/company/mailboxes/{id}/grants',
                    'GET /api/v1/company/templates',
                    'POST /api/v1/company/mailboxes/{id}/convert-shared',
                    'POST /api/v1/company/mailboxes/{id}/handover',
                    'POST /api/v1/company/templates/{id}/retire',
                    'PUT /api/v1/company/mailboxes/{id}/grants'],
 'closure_paths': ['docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
                   'web/app/(dashboard)/company/templates/page.tsx',
                   'web/components/company/grants.tsx'],
 'source_paths': ['internal/app/mailindex/service.go',
                  'internal/ingest/recovery.go',
                  'internal/ingest/recovery_read.go',
                  'internal/smtp/server.go',
                  'web/app/(dashboard)/company/templates/page.tsx',
                  'web/components/company/grants.tsx']}


REVISION7_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': '86df3d6e8e9807b19fc5b907c7385fef80c6b8fd',
                 'sha256': '9ad325330fc53f4ded0ea5a20ae25c85bfc2026c6274a3ef78815a36d95859cb'},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': '1ae3373eb5b57c95e219420995558bb0afe68c8f',
                   'sha256': '115294363d6c05a88e1bf08c535a7d4411f8e9409fe9b854dd71821760ec44f7'},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': 'b82508daaf8327f6f49b7441e97f43d3c8f1199b',
             'sha256': 'e818ee078423dce13e46d5a314dddd1a9d4dd28dbf1c6162a900218f177e9a4d'},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': '28d3de48355fc35b585d47cd02a63f2886efad92',
                   'sha256': 'cf3b5a326bfb9e28f92bab28cfa98145762505465142dc86fe0a4f4703106c3d'}}

REVISION8_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': '8ba42265c61b74a1e40bb29c3e302b811cfc2f97',
                 'sha256': '52477ba99dbec5eeb0d900d98be039cc383acb0c7284b269fcd0f71ea53cc404'},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': '394dd577f1a08603b65546e904a637ad2eb80cf0',
                   'sha256': '46eca04fa364ff6bf575b5828e6219b9b6b6e217d6ac836a9c1e35fa450dcd2d'},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': '40ecfe14652c5dda8fa4de077e316913f6505c9a',
             'sha256': '7cc241cd0a0b83cab845bee62697a4301fa73a7de8115d3a56b09fbcbec64a60'},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': 'd52027ee633aad43b38814ab6db7ef6429c35a2c',
                   'sha256': '1ad8439d35d67255c0bf9694c095ecbf8210506d26f6f15232d28276f8604c85'}}

REVISION8_REVIEW_BLOB = 'c280a9c1bc62c2ebe657349a5bc6b63bc07b997c'
REVISION8_REVIEW_SHA256 = '66408569479fa4390adbd023980d8160b36d15c6c335da9cbbcb11e74b7c969b'

REVISION8_EXPECTED = {'transaction_callers_sha256': 'd76503909d48bcf942cdf11ddf3dc6a00efecddaabe4709a15f46743a8384322',
 'transaction_syntax_sha256': 'ca75adc3160805ef27a3f368a545e15f3f1350e7fde154f93202a5d025747173',
 'transaction_review_fields_sha256': '0c756954f8f45c80fc9ec2ed6bb839f158e7f48629c8396d531a6fb3161c6489',
 'source_paths': ['internal/api/middleware/ratelimit.go',
                  'internal/app/admin/service.go',
                  'internal/app/companymail/service.go',
                  'internal/app/templates/service.go',
                  'internal/ratelimit/sliding.go',
                  'internal/rawobject/store.go',
                  'internal/retention/scanner.go',
                  'internal/store/postgres/company_templates.go',
                  'web/app/(dashboard)/account/page.tsx',
                  'web/components/company/send-policy.tsx',
                  'web/features/company/mailbox-admin.tsx',
                  'web/features/mail/components/received-folder.tsx'],
 'source_closure_sha256': '1f3a76471f5efe1a7b390cbac7c734ded68e393d9bc5b097754afb9e3ca7764d',
 'client_changes': [{'before_index': 39,
                     'after_index': 39,
                     'path': '/api/v1/company/mailboxes',
                     'methods': ['POST'],
                     'changes': {'line': {'before': 48, 'after': 65}}}],
 'closure_paths': ['docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
                   'internal/api/middleware/ratelimit.go',
                   'web/features/company/mailbox-admin.tsx']}

REVISION8_REVIEWED_SUBJECT = {'enabled_true': 'activeCompanyUser retains the active same-tenant target-user requirement.',
 'enabled_false': 'A new SELECT EXISTS users predicate on tenant_id and id permits revoking an existing '
                  'frozen employee; a missing or foreign-tenant target returns BadRequest.',
 'transaction_order': 'companyTx acquires the tenant write lock, rereads the current actor, requires current '
                      'company administration, then checks mailbox access, target-user eligibility, and '
                      'template existence. The grant DELETE remains scoped to tenant, template, mailbox and '
                      'user.',
 'atomicity': 'Grant mutation, mandatory companyAudit and outbox insert remain in the same transaction; '
              'errors return before commit.',
 'qualification': 'static source inspection only; no PostgreSQL, concurrency, wire, dependency, release or '
                  'parent-task acceptance'}


REVISION9_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': '0a2c62c81cf88214d31d98e41543750cd5b4f470',
                 'sha256': '32b35746b6b2ef170ae423fa0d460fb7375a229b0e402cb206f39bd1a63d8907'},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': '739574b45bcb1715e7a10c478c8fa3c85e095d2f',
                   'sha256': '1af19406cb5bad0df43b1b36fc95310dfa28566ceb5b4ce8cbb9e7577f1bb314'},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': 'f6eb4c08f02e313d230ef97f8f02f1b2f3bf2b55',
             'sha256': 'c62f46e2b95aeea514050a1f4a09308f7d9be5815a1640bded077605abae9057'},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': 'd8dcb3a29190e689612eeac383e4f5b8f3482e8c',
                   'sha256': '11196f0fce4773d317d53b12ecf107ae21f5fbeb0fefe2c27c84e37571ca625d'}}

REVISION9_REVIEW_BLOB = '6af5018154c5690de6ce0c032d4151f1a03fbcaa'
REVISION9_REVIEW_SHA256 = '6d591df4d48ece5d08384ac56a30e9f29acac27ec33876d8eee8dabb0d1f4b02'

REVISION9_EXPECTED = {'transaction_callers_sha256': '5fcd8852f8c70784c5fa578b3710a2ee010844cbe5f3b27910d79e2892664c32',
 'client_changes_sha256': '9d810acf5272cf840ad55752ef6ce6e0440baedccc9ea892337c4e8b0e52ae5b',
 'compatibility_routes_sha256': '0d31cda4586691274f33e7da9c5d59029c6d357ffcd52fe2212fdf844e5f6b10',
 'closure_changes_sha256': '9713c99d84e32f3c5281b254b2a799106fafc684fcd0261c33dbf0b616a3baed',
 'source_changes_sha256': '78575ab4911deb697bc1208471a4c5d6f262b1dbb36b77c4a30a6aabd0e94152',
 'source_paths': ['internal/api/handlers/admin.go',
                  'internal/api/handlers/auth.go',
                  'internal/app/permissions/editor.go',
                  'internal/company/permission_editor.go',
                  'internal/hooks/dispatcher.go',
                  'internal/ingest/service.go',
                  'internal/outbound/delivery.go',
                  'web/components/company/employee-field.tsx',
                  'web/features/company/mailbox-admin.tsx',
                  'web/features/company/profile-management.tsx',
                  'web/features/company/user-management.tsx'],
 'historical_manifest_sha256': '5d28b8c615ea00e3233e3135a22d024ae6297abbd7e9c65ba0bcf5ac60516a18',
 'protected_source_manifest_sha256': '346002800422bdf202a816d1f780467cad2761351d4cda5a387dc7f5572a4777',
 'compatibility_rejections': {'revision1': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/admin/runtime-config',
                              'revision2': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/admin/runtime-config',
                              'revision3': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/admin/runtime-config',
                              'revision4': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/company/mailboxes/{id}/grants',
                              'revision5': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/company/mailboxes/{id}/grants',
                              'revision6': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/company/mailboxes/{id}/grants',
                              'revision7': 'route/schema/client/test source '
                                           'drift: POST '
                                           '/api/v1/company/mailboxes',
                              'revision8': 'route/schema/client/test source '
                                           'drift: POST '
                                           '/api/v1/company/mailboxes'},
 'transaction_revision8_rejection': 'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:CreateAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:DeleteAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:ListAPIKeys\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:ListAPIKeysByOwner\n'
                                    'caller drift: '
                                    'internal/store/postgres/messages.go:*PgStore:CountTenantMessagesSince\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetUser\n'
                                    'caller drift: '
                                    'internal/store/postgres/permission_assignment.go:*PgStore:AssignPermissionEditor\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:CreatePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:DeletePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:ListPlans\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:UpdatePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/postgres.go:*PgStore:Close\n'
                                    'caller drift: '
                                    'internal/store/postgres/postgres.go::New\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:CountDeadWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:CreateOutboxEvent\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:CreateWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListDeadWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListIngestJobs\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/refresh_rotation.go:*PgStore:RevokeRefreshTokenByHash\n'
                                    'caller drift: '
                                    'internal/store/postgres/refresh_rotation.go:*PgStore:RotateRefreshToken\n'
                                    'caller drift: '
                                    'internal/store/postgres/settings.go:*PgStore:ListSettings\n'
                                    'caller drift: '
                                    'internal/store/postgres/smtp_policy.go:*PgStore:GetSMTPPolicy\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:CreateTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:DeleteTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:EffectiveConfig\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:ChangePasswordAtomic\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:CreateRefreshToken\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:CreateUser\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:GetUser\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:GetUserByEmail\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:RevokeUserRefreshTokens\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:TouchUserLogin\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:UpdateUserPassword\n'
                                    'caller drift: '
                                    'internal/store/postgres/webhook_endpoints.go:*PgStore:ListWebhookEndpoints'}


REVISION10_EXPECTED = {'transaction_callers_sha256': '482df905525bccc17bd1f5c8d8d19f25c9e9046b0fa62444ab4ee20f41b44560',
 'client_changes_sha256': '37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570',
 'compatibility_routes_sha256': '37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570',
 'closure_changes_sha256': 'a539fb4649c41cc661b0b924e9e9c90785269081078f76f33ed6f2fd4d44a1d5',
 'source_changes_sha256': 'ebb072e9029fadd9817013e897dbd5b21e52dba40f2bfbf3a5657c9758c11b41',
 'source_paths': ['internal/api/handlers/company_stream.go',
                  'internal/api/handlers/monitor.go',
                  'internal/app/admin/service.go',
                  'internal/app/companymail/attachment_read.go',
                  'internal/app/templates/service.go',
                  'internal/config/config.go',
                  'internal/ingest/content.go',
                  'internal/ingest/recovery.go',
                  'internal/ingest/service.go',
                  'internal/models/mailbox_grants.go',
                  'internal/settings/settings.go',
                  'web/app/(dashboard)/admin/plans/page.tsx',
                  'web/app/(dashboard)/admin/policy/page.tsx',
                  'web/features/mail/components/rich-message.tsx',
                  'web/locales/en.json',
                  'web/locales/zh.json'],
 'historical_manifest_sha256': '082ae071dd5a8967c8cd114ca1eb7d5fe1defda9b77c3895b7073a88f407933f',
 'protected_source_manifest_sha256': '346002800422bdf202a816d1f780467cad2761351d4cda5a387dc7f5572a4777',
 'compatibility_rejections': {'revision1': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/admin/runtime-config',
                              'revision2': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/admin/runtime-config',
                              'revision3': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/admin/runtime-config',
                              'revision4': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/company/mailboxes/{id}/grants',
                              'revision5': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/company/mailboxes/{id}/grants',
                              'revision6': 'route/schema/client/test source '
                                           'drift: GET '
                                           '/api/v1/company/mailboxes/{id}/grants',
                              'revision7': 'route/schema/client/test source '
                                           'drift: POST '
                                           '/api/v1/company/mailboxes',
                              'revision8': 'route/schema/client/test source '
                                           'drift: POST '
                                           '/api/v1/company/mailboxes',
                              'revision9': 'source hash drift'},
 'transaction_revision9_rejection': 'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:CreateAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:DeleteAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:GetAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:ListAPIKeys\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:ListAPIKeysByOwner\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:InsertAudit\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:ListAuditEntries\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:ListAuditEntriesPaged\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:ListMonitorEvents\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_mail.go:*PgStore:ListMailboxEvents\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_members.go:*PgStore:GetCompanySettings\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_members.go:*PgStore:GetWorkMailbox\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_templates.go:*PgStore:TemplateForSend\n'
                                    'caller drift: '
                                    'internal/store/postgres/ingress.go:*PgStore:DeliverIngress\n'
                                    'caller drift: '
                                    'internal/store/postgres/mailboxes.go:*PgStore:CountAllMailboxes\n'
                                    'caller drift: '
                                    'internal/store/postgres/messages.go:*PgStore:CountAllMessages\n'
                                    'caller drift: '
                                    'internal/store/postgres/messages.go:*PgStore:CountTenantMessagesSince\n'
                                    'caller drift: '
                                    'internal/store/postgres/messages.go:*PgStore:EnqueueOrphanRetry\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetZone\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:TemplateForSend\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:CreatePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:DeletePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:GetPlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:ListPlans\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:UpdatePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/postgres.go:*PgStore:Close\n'
                                    'caller drift: '
                                    'internal/store/postgres/postgres.go::New\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListIngestJobs\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/settings.go:*PgStore:ListSettings\n'
                                    'caller drift: '
                                    'internal/store/postgres/smtp_policy.go:*PgStore:GetSMTPPolicy\n'
                                    'caller drift: '
                                    'internal/store/postgres/smtp_policy.go:*PgStore:UpsertSMTPPolicy\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:CreateTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:DeleteTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:GetTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:ListTenants\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:UpsertOverride\n'
                                    'caller drift: '
                                    'internal/store/postgres/zones.go:*PgStore:CountAllZones\n'
                                    'caller drift: '
                                    'internal/store/postgres/zones.go:*PgStore:GetZone',
 'transaction_revision8_rejection': 'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:CreateAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:DeleteAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:GetAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:ListAPIKeys\n'
                                    'caller drift: '
                                    'internal/store/postgres/apikeys.go:*PgStore:ListAPIKeysByOwner\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:InsertAudit\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:ListAuditEntries\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:ListAuditEntriesPaged\n'
                                    'caller drift: '
                                    'internal/store/postgres/audit.go:*PgStore:ListMonitorEvents\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_mail.go:*PgStore:ListMailboxEvents\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_members.go:*PgStore:GetCompanySettings\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_members.go:*PgStore:GetWorkMailbox\n'
                                    'caller drift: '
                                    'internal/store/postgres/company_templates.go:*PgStore:TemplateForSend\n'
                                    'caller drift: '
                                    'internal/store/postgres/ingress.go:*PgStore:DeliverIngress\n'
                                    'caller drift: '
                                    'internal/store/postgres/mailboxes.go:*PgStore:CountAllMailboxes\n'
                                    'caller drift: '
                                    'internal/store/postgres/messages.go:*PgStore:CountAllMessages\n'
                                    'caller drift: '
                                    'internal/store/postgres/messages.go:*PgStore:CountTenantMessagesSince\n'
                                    'caller drift: '
                                    'internal/store/postgres/messages.go:*PgStore:EnqueueOrphanRetry\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetAPIKey\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetUser\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:GetZone\n'
                                    'caller drift: '
                                    'internal/store/postgres/outbound_retry_reader.go:*outboundRetryReader:TemplateForSend\n'
                                    'caller drift: '
                                    'internal/store/postgres/permission_assignment.go:*PgStore:AssignPermissionEditor\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:CreatePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:DeletePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:GetPlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:ListPlans\n'
                                    'caller drift: '
                                    'internal/store/postgres/plans.go:*PgStore:UpdatePlan\n'
                                    'caller drift: '
                                    'internal/store/postgres/postgres.go:*PgStore:Close\n'
                                    'caller drift: '
                                    'internal/store/postgres/postgres.go::New\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:CountDeadWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:CreateOutboxEvent\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:CreateWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListDeadWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListIngestJobs\n'
                                    'caller drift: '
                                    'internal/store/postgres/queue.go:*PgStore:ListWebhookDeliveries\n'
                                    'caller drift: '
                                    'internal/store/postgres/refresh_rotation.go:*PgStore:RevokeRefreshTokenByHash\n'
                                    'caller drift: '
                                    'internal/store/postgres/refresh_rotation.go:*PgStore:RotateRefreshToken\n'
                                    'caller drift: '
                                    'internal/store/postgres/settings.go:*PgStore:ListSettings\n'
                                    'caller drift: '
                                    'internal/store/postgres/smtp_policy.go:*PgStore:GetSMTPPolicy\n'
                                    'caller drift: '
                                    'internal/store/postgres/smtp_policy.go:*PgStore:UpsertSMTPPolicy\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:CreateTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:DeleteTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:EffectiveConfig\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:GetTenant\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:ListTenants\n'
                                    'caller drift: '
                                    'internal/store/postgres/tenants.go:*PgStore:UpsertOverride\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:ChangePasswordAtomic\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:CreateRefreshToken\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:CreateUser\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:GetUser\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:GetUserByEmail\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:RevokeUserRefreshTokens\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:TouchUserLogin\n'
                                    'caller drift: '
                                    'internal/store/postgres/users.go:*PgStore:UpdateUserPassword\n'
                                    'caller drift: '
                                    'internal/store/postgres/webhook_endpoints.go:*PgStore:ListWebhookEndpoints\n'
                                    'caller drift: '
                                    'internal/store/postgres/zones.go:*PgStore:CountAllZones\n'
                                    'caller drift: '
                                    'internal/store/postgres/zones.go:*PgStore:GetZone'}



REVISION10_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': '948ae430554121c8ac68bed83fe32d576c0839e2',
                 'sha256': '270efaae609d5cd16b43272806428e4306353762df40c2a63d4f52487d78f438',
                 'bytes': 2801187},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': '148068966b895865d6e4caaee174f60050456602',
                   'sha256': '125d40cfdc3f0abc500e5286cbf27abb44f5443868fc32447dce5eeea8839137',
                   'bytes': 406948},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': 'f6eb4c08f02e313d230ef97f8f02f1b2f3bf2b55',
             'sha256': 'c62f46e2b95aeea514050a1f4a09308f7d9be5815a1640bded077605abae9057',
             'bytes': 40832},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': 'd8dcb3a29190e689612eeac383e4f5b8f3482e8c',
                   'sha256': '11196f0fce4773d317d53b12ecf107ae21f5fbeb0fefe2c27c84e37571ca625d',
                   'bytes': 49631}}

REVISION10_REVIEW = {'path': 'docs/company-mail/evidence/R5-CATALOG-REVISION10-20261008/reconciliation.json',
 'blob': '424bb8ee441fa40e1ff3bc19dba3c8d993b1ca44',
 'sha256': '2ce2c1339ac03dc3673f5cb6e9868dfa1bc943cdf691782ce218241cd292e953',
 'bytes': 71208}

REVISION11_EXPECTED = {'review_field_sha256': {'transaction_callers': '64cb2a3f7190e3b331d0c88ac7079d94d7c5db757fda85065d10c8a9bc6fb7ff',
                         'transaction_existing_entry_deltas': '393c986645c85dd6bea8bb60afc16abe626c162ab81d74f1fec83c245e513b33',
                         'transaction_added_function_ids': 'de039ef7b643815ab68fe9b83065e7d03bff81f724902d5c99bea510333205c3',
                         'transaction_syntax_changes': '562944c8cb7a5b14e8120ecbae533c9a78ab75f64d0ab4a9904874e428f772e7',
                         'transaction_manual_review_changes': '9d1df0fe3fbc4259abfb8c4eb7964f02a969017bfca719401e51e753d24fbb7d',
                         'transaction_file_review_change': '39998ebcabbfaec55af6f2da77c8ed11d0aec7d80d47c822171fb77d0d41034a',
                         'client_previous_indices': '99a5be0103bb2b95d815c85ae28105282294d33c821f2f0167841b6d0c89a67a',
                         'client_changes': '12b267f78c9d67e3a319f281501f3ea091f8e0051871a8d63e4d61561c766a0d',
                         'client_added': 'e3c1e4aa4985a6977b6781ac72aee10fdc65d48b361e24bdef5daa1fc57c6556',
                         'compatibility_routes': 'b7c39ab372b0aee4f864777658751a0de08ae86942ed23f64acb40d686ffa97e',
                         'new_route_review': '0825350135ea979c8cbd5fdc08ec507f96f55367d0cb592cea24558ae5f1c545',
                         'closure_changes': 'c9ecbee74bc675715bb262e41ec66d9d25e050f092b44c2d7069e5b9dc5c0a51',
                         'closure_added': 'b3b674cf0abd5d60f99cdc1606e5549c9708bc18bd2785e83fba269948c548ad',
                         'source_changes': '12f5d97c40e328b8b63753a181c0ffc10731c45e2a7ea964733727be10545a36',
                         'protected_source_changes': '983fbf7f6626c17890607551a264bfab7f21e65f040617e01ee9edbbc6905138',
                         'historical_manifest': '690c1c57cb8d06dad1bcf772da404194069209c820f3a6dfc6ef64a8787631e5',
                         'protected_source_manifest': 'cd664d742e3f96b50e4db3bc5aa4c575f9958a765f6ce4090675bea9f0edfb27',
                         'current_rejections': '29ceb0cb7ad65622fcfbe6993ccee536a3e14decdeb2fc5d54ccc0fd8576bc1a',
                         'transaction_body_changes': 'b3fa6c4f2af409ef98b214bad1d6eebed7252b34c929061a94315ab2dcb30351',
                         'zone_function_review': '7f954bdb021967cb4649c07968205afdd3e37365fa3047a0844f82e55b6c31e1',
                         'additional_build_metadata_changes': 'b1423651e06568fa24371e6dc4f77ac0065760cfc6d6cd41966c3a4ab2dd1e84',
                         'incremental_changes_since_3a': 'd12bcee54543d28ab45c77db48d7462f8e69f3612281925e600fbe6da713604e',
                         'intermediate_revision11': '145690dd781417fb353312b50723514a38b6f3369021ba7c31890baaa781d27a'},
 'transaction': {'status': 'PASS',
                 'postgres_files': 63,
                 'functions': 401,
                 'sql_execution_calls': 501,
                 'direct_write_functions': 138,
                 'write_closure_functions': 158,
                 'migration_files': 19,
                 'task_complete': False,
                 'runtime_verified': False,
                 'meaning': 'syntax inventory current; no concurrency or behavior equivalence claim'},
 'new_function_source_sha256': {'internal/store/postgres/queue.go:*PgStore:MarkOutboxEventDoneClaim': 'd6226de9a10ad7e6903e78bceace05f4c3036168f84c24115024193432fe46d7',
                                'internal/store/postgres/queue.go:*PgStore:MarkOutboxEventRetryClaim': '4fc859da8dc1bf9d34514909e0751e19028458dd2d4bdecfc72edd03cf6a70c2',
                                'internal/store/postgres/queue.go:*PgStore:MarkWebhookDeliveryDoneClaim': '7df521485bf4ae5fec0c9cb77bc25d28247d77d8f780530f5f8d4e187b02c923',
                                'internal/store/postgres/queue.go:*PgStore:MarkWebhookDeliveryRetryClaim': '598ef068b3b388c652595421459c2632b7c495568aa99306fe1545f77857b0d3',
                                'internal/store/postgres/queue.go:*PgStore:markQueueClaim': 'b5f93fb0871e879ec79e2d68f9021ba3f0b603dfc1f4499f536fad96a755699c',
                                'internal/store/postgres/zone_errors.go::classifyZoneCreateError': '49772c79c228e17b93d6dd454300fe83c6e421e1b9fca806655e1d2f3f115e6c'},
 'new_function_review_sha256': 'bf591e14ba46c5b440caa40a1289339598bd8f0985d1861b9c1426c2d6255a63',
 'generated_catalog_sha256': {'transaction': '7b4737f5b1737206ededeb415a3fe6d69de94ff27ff51a24d93893e01cd60417',
                              'compatibility': '3d002d43a4d8432b4f2f162f0dde3e96bdcd4433cd391c23b13f79fd659e6434',
                              'clients': '84282d32b16d1dfc66a37c48ebb4b14fb2aafced02d72b906f5defcac12c805e',
                              'client_routes': 'd839c48614f2a329ac97680b5d2ba49180af2b20900d12ffe77cd74a9286788f'},
 'zone_function_review_sha256': '77c8accf75ace5594aa46729e5a7b4a46c55200d7d849dbac57722d62eaa5450'}



REVISION11_PRIOR_PRODUCT_COMMIT = '3a103cda4363cb8f32839eaaa73ab065eb967f79'
REVISION11_PRIOR_CORE_PUBLIC_COMMIT = '9bcc542dcaa0bb7c280c380ea278c1a2a52717d3'
REVISION11_PRIOR_EVIDENCE_PUBLIC_COMMIT = 'f2713367dd949487c632ebee51de3a0cc1d2c6f7'


REVISION11_COMMIT = '58d0c9cf274569258319ff4b5dcab7912b0174f4'
REVISION11_TREE = 'e4c4550481d9dc9efc5d4d849f85d496b99f65c4'
REVISION11_SOURCE_COMMIT = '33f61fde3cf37d3cec2dd360146369c291eb0930'
REVISION11_SOURCE_TREE = 'd982493e28431e644fd4018d8f746d8940280268'
REVISION11_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': 'd6aacdc204f8d068f97aabd9eec5fd92b996fb4c',
                 'sha256': '7b4737f5b1737206ededeb415a3fe6d69de94ff27ff51a24d93893e01cd60417',
                 'bytes': 2863377},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': 'acadb64272926afd0184de1840ef13826a4886d2',
                   'sha256': '3d002d43a4d8432b4f2f162f0dde3e96bdcd4433cd391c23b13f79fd659e6434',
                   'bytes': 409515},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': '0c9d99c7b0cdcc14b8cb94ea505a047691086380',
             'sha256': '84282d32b16d1dfc66a37c48ebb4b14fb2aafced02d72b906f5defcac12c805e',
             'bytes': 41118},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': '90a1af36910b27a388e1b8b05f37551fbe12db66',
                   'sha256': 'd839c48614f2a329ac97680b5d2ba49180af2b20900d12ffe77cd74a9286788f',
                   'bytes': 49979}}
REVISION12_EXPECTED = {'transaction': {'status': 'PASS',
                 'postgres_files': 63,
                 'functions': 401,
                 'sql_execution_calls': 501,
                 'direct_write_functions': 138,
                 'write_closure_functions': 158,
                 'migration_files': 19,
                 'task_complete': False,
                 'runtime_verified': False,
                 'meaning': 'syntax inventory current; no concurrency or behavior equivalence claim'},
 'compatibility': {'wire_validation_scope': 'not_checked_current_wire_required',
                   'historical_wire_reference': {'artifact_ref': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-map-v1.json',
                                                 'sha256': '61b039486bc7804366012298fe87203882b6fb52ba1b160eb8ee77d76989dc22',
                                                 'qualification': 'historical_metadata_only_not_current_wire'},
                   'status': 'source_inventory_and_upgrade_plan_checked',
                   'task_complete': False,
                   'product_green': False,
                   'routes': 133,
                   'client_branches': 136,
                   'source_files': 95,
                   'openapi_missing': ['DELETE /api/v1/suppression/{id}',
                                       'GET /api/v1/suppression',
                                       'GET /docs-assets/*'],
                   'no_shipped_client': ['DELETE /api/v1/suppression/{id}',
                                         'GET /api/v1/admin/status',
                                         'GET /api/v1/auth/me',
                                         'GET /api/v1/company/outbound/{id}/recipients',
                                         'GET /api/v1/suppression',
                                         'GET /docs',
                                         'GET /docs-assets/*',
                                         'GET /metrics',
                                         'GET /openapi.yaml',
                                         'GET /ready',
                                         'GET /redoc'],
                   'runtime_boundary': 'No HTTP/DB/old-client upgrade execution; fresh scoped evidence and dependency '
                                       'review remain required.'},
 'review_field_sha256': {'transaction_callers': '632cb2aff9b8b91c5749eedcbfd1ad0e3b9eb99957541a46a99393c10c74acfe',
                         'compatibility_route_changes': '353a2d9abff84b88754e8e6e8656f654b74f9d69cfda434c572162f32fb46e6d',
                         'closure_changes': 'ff53d2ff5ff61b0974ea00ee6104c31da8d46c51dc3176c573c44427f98de0ee',
                         'source_changes': 'abac9d725991d8bbd81a008c5364f40792ef806efe460ebe591ff178099c0517',
                         'current_rejections': '17c998a1a320ec6cd64ada6d74b240f0de7c16dd5a18986edea19afd052e0f2b',
                         'generated_catalogs': 'adc50ed72941fad60e540dfd10069c80b67a3072deb956ac9eed78728c9c388f',
                         'additional_api_schema_changes': 'e501586bcf9fb86e00490ef31f077af3d3867541e217fa58d52dfc2192beab10'},
 'historical_manifest_sha256': '3f0fd50e1b86ff3e470b5beca0399cf72791cb88d90e130e63b4f3119d47c53a',
 'protected_source_manifest_sha256': 'd9e16753906d1372bbfdfca9f7d1352c9b7c98511e30b970c3c53752d1402f1c',
 'product_paths': ['internal/api/handlers/company_stream.go',
                   'internal/api/middleware/ratelimit.go',
                   'internal/app/companymail/service.go',
                   'internal/app/domains/service.go',
                   'internal/configcache/configcache.go',
                   'internal/dkim/dkim.go',
                   'internal/mailcontent/parser.go',
                   'internal/mailcontent/source_identity.go',
                   'internal/metrics/metrics.go',
                   'internal/models/models.go',
                   'web/app/(dashboard)/admin/page.tsx',
                   'web/features/company/audit.tsx',
                   'web/features/company/domain-settings.tsx',
                   'web/features/company/employees.tsx',
                   'web/features/company/mailbox-admin.tsx',
                   'web/lib/types.ts',
                   'web/locales/en.json',
                   'web/locales/zh.json']}

def revision11_snapshot(name):
    pin = REVISION11_SNAPSHOTS[name]
    ref = REVISION11_COMMIT + ':' + pin['path']
    raw = git('show', ref)
    if git('rev-parse', ref).decode().strip() != pin['blob'] or hashlib.sha256(raw).hexdigest() != pin['sha256'] or len(raw) != pin['bytes']:
        raise ValueError('revision11 public historical catalog identity drift: ' + name)
    return json.loads(raw)



REVISION12_COMMIT = 'b7247c3f66bd5f0ec5c6390305e2ff7268c135f8'
REVISION12_TREE = 'd9cce4ad1fe27c66e8954346894b69516364da4d'
REVISION12_SOURCE_COMMIT = 'ed81ee2fcadc9e8cfcd165947561f8b4b65589e6'
REVISION12_SOURCE_TREE = '3abdbfca4c0318644a01755c6e6bbb1929b74633'
REVISION12_SNAPSHOTS = {'transaction': {'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                 'blob': '09d37f550af34fb94b653c8b401649a9a70d56c1',
                 'sha256': '2fd33651488b60694657ae45b9bd34f03fe1015bdd4cbfecd4afb196543169ac',
                 'bytes': 2863488},
 'compatibility': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
                   'blob': '74b64a51552302ab5155b5968568ee7d95aaeac2',
                   'sha256': '05293f4608ef7066b3e2257bf00e20a7347aa7914e6f63a172cb070789e43237',
                   'bytes': 409919},
 'clients': {'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
             'blob': '8c8bdf49c2dea6828d690f6b73cdd79aaac2b952',
             'sha256': 'e7bfb4952e0af33ab415997519a8c4d89941f8c4ff6a8329f9dab1fd35fb01a3',
             'bytes': 41444},
 'client_routes': {'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
                   'blob': '7ccbfcbec2697cf513ee47f8d84938be16844f41',
                   'sha256': 'a1ec852e3a126f41394685c19c43b94d8f6b9d333e017dbf11f67aaaa3e1c39d',
                   'bytes': 50365}}
REVISION13_EXPECTED = {'transaction': {'status': 'PASS',
                 'postgres_files': 63,
                 'functions': 401,
                 'sql_execution_calls': 501,
                 'direct_write_functions': 138,
                 'write_closure_functions': 158,
                 'migration_files': 19,
                 'task_complete': False,
                 'runtime_verified': False,
                 'meaning': 'syntax inventory current; no concurrency or behavior equivalence claim'},
 'compatibility': {'wire_validation_scope': 'not_checked_current_wire_required',
                   'historical_wire_reference': {'artifact_ref': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-map-v1.json',
                                                 'sha256': '61b039486bc7804366012298fe87203882b6fb52ba1b160eb8ee77d76989dc22',
                                                 'qualification': 'historical_metadata_only_not_current_wire'},
                   'status': 'source_inventory_and_upgrade_plan_checked',
                   'task_complete': False,
                   'product_green': False,
                   'routes': 133,
                   'client_branches': 136,
                   'source_files': 95,
                   'openapi_missing': ['DELETE /api/v1/suppression/{id}',
                                       'GET /api/v1/suppression',
                                       'GET /docs-assets/*'],
                   'no_shipped_client': ['DELETE /api/v1/suppression/{id}',
                                         'GET /api/v1/admin/status',
                                         'GET /api/v1/auth/me',
                                         'GET /api/v1/company/outbound/{id}/recipients',
                                         'GET /api/v1/suppression',
                                         'GET /docs',
                                         'GET /docs-assets/*',
                                         'GET /metrics',
                                         'GET /openapi.yaml',
                                         'GET /ready',
                                         'GET /redoc'],
                   'runtime_boundary': 'No HTTP/DB/old-client upgrade execution; fresh scoped evidence and dependency '
                                       'review remain required.'},
 'review_field_sha256': {'transaction_callers': 'afae974864809fdb9c842a60d2374d674268ba467fa67f2db3de0efbfc1bcfa1',
                         'compatibility_route_changes': 'c10defac3f8ea053f643aa36e63b3dc1ea618919f83480634408d12da7e7f525',
                         'closure_changes': '8571422dc5462b8afa5ad8772a1c0de7f7e738895adabd7f2a1d7106923e1328',
                         'source_changes': '6905652c09419ea324c13dfeb57a51069038fc4f11a350fd06da84cb08ddce30',
                         'current_rejections': '47ba8f8e76de292774bad44fc55339cff91942e024921a4df88c2d47cfc95395',
                         'generated_catalogs': 'ecd3692c4937f44b967560676e6de1600c6d407e9c307b65ad8b75e071d49b04',
                         'additional_api_schema_changes': '37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570',
                         'transaction_body_changes': '5a7f51baf11af1afb975b9bce356e604db9178cd0b7f42ed852a76c57ac3a152',
                         'transaction_classification_changes': 'cc9df710835f85accd001a7f55c3ef688cf092e0f09a7f86925180703278a64a',
                         'transaction_entry_changes': '03b5a41a660f88539fa55562e2df97ccf6a5bd17d47c5d43f20c80851f02528a'},
 'historical_manifest_sha256': '9abddc05e27d59685b9fc56e49c48953d361e46c8bf474b5a51619cf2f0cdc8a',
 'protected_source_manifest_sha256': '221d6fe7938cf8785d74505515b979b9cda29d760289f472ca82ecd0e1469966',
 'product_paths': ['internal/api/handlers/auth.go',
                   'internal/api/middleware/ratelimit.go',
                   'internal/app/admin/service.go',
                   'internal/app/submissions/service.go',
                   'internal/app/templates/service.go',
                   'internal/config/config.go',
                   'internal/models/models.go',
                   'internal/models/refresh_issuance.go',
                   'internal/outbound/delivery.go',
                   'internal/outbound/delivery_context.go',
                   'internal/outbound/recipient_address.go',
                   'internal/outbound/recipient_delivery.go',
                   'internal/outbound/service.go',
                   'internal/smtp/server.go',
                   'internal/store/postgres/users.go',
                   'internal/store/store.go',
                   'internal/testutil/fake_store_refresh.go',
                   'web/components/company/compose.tsx',
                   'web/components/company/templates/editor.tsx',
                   'web/features/mail/components/conversation-list.tsx',
                   'web/features/mail/components/draft-folder.tsx',
                   'web/features/mail/components/legacy-receipt-folder.tsx',
                   'web/features/mail/components/list-controls.tsx',
                   'web/features/mail/components/receipt-folder.tsx',
                   'web/features/mail/components/received-folder.tsx',
                   'web/features/mail/components/rich-message.tsx',
                   'web/features/mail/components/sent-folder.tsx'],
 'refresh_manual_fields': {'lock_fk_wait_fence': {'boundary': 'Issuance proof present: Begin -> user FOR SHARE -> '
                                                              'compare IDs/password/session_version/active -> refresh '
                                                              'INSERT -> Commit; the user lock is held until '
                                                              'transaction end. Nil proof keeps the existing internal '
                                                              'pool-statement path; current production issueTokenPair '
                                                              'always supplies proof.',
                                                  'local_operations': [{'line': 155,
                                                                        'operation': 'tx.QueryRow',
                                                                        'sql_expression': 'userSelect + ` WHERE id=$1 '
                                                                                          'FOR SHARE`',
                                                                        'status': 'lexical-source-only'},
                                                                       {'line': 171,
                                                                        'operation': 'exec',
                                                                        'sql_expression': None,
                                                                        'status': 'lexical-source-only'}],
                                                  'direct_lock_fragments': [{'line': 155,
                                                                             'sql_fragment': ' WHERE id=$1 FOR SHARE'}],
                                                  'implicit_fk': 'refresh_tokens.user_id references the already '
                                                                 'SHARE-locked users row. The issuance transaction '
                                                                 'takes no tenant/audit/family lock after this row; '
                                                                 'unknown external triggers are not certified.',
                                                  'dynamic_execution': 'The unchanged AST SQL-call-name collector sees '
                                                                       'tx.QueryRow; exec(ctx, INSERT...) is a '
                                                                       'function-valued alias selected from pool.Exec '
                                                                       'or tx.Exec and is manually reviewed, not '
                                                                       'counted as type-resolved SQL execution.'},
                           'evidence': ['internal/store/postgres/users.go:137-182 '
                                        'sha256=66e6bd4224e13273b7bf3904de303f128c1d4cfb980f1773b24ebf879198f1d0',
                                        'PR #179 / Issue #170: actual PostgreSQL 16 fixed 9-leaf red-green; catalog '
                                        'remains source-only and does not replace parent gates.'],
                           'unverified_risks': ['Nil-proof compatibility callers must not become interactive issuance '
                                                'entry points; the current sole production caller always supplies '
                                                'proof.',
                                                'The fixed PostgreSQL cancellation control stops at the user-read '
                                                'barrier, not an in-flight INSERT. Commit-acknowledgement loss and all '
                                                'unrelated transaction relations remain outside this review.'],
                           'file_family_context': 'ChangePassword定位tenant→T KEY SHARE→U条件W→refresh W→audit；RevokeUser '
                                                  'U UPDATE→R；旧DeleteUser Key DELETE→U '
                                                  'DELETE；旧CRUD保持；有proof的refresh创建在用户SHARE锁事务中，nil-proof内部兼容为单SQL',
                           'source_review': {'base_commit': '9b73b13f376f7e8d589a06fc7078dae4c342d5d5',
                                             'source_sha256': '66e6bd4224e13273b7bf3904de303f128c1d4cfb980f1773b24ebf879198f1d0',
                                             'review': 'docs/company-mail/evidence/R5-CATALOG-REVISION13-20261008/README.md',
                                             'qualification': 'static-type-review; actual login issuance evidence is '
                                                              'separately scoped in PR179',
                                             'historical_manual_fields_preserved_in': 'historical_revision12_review'}}}

def revision12_snapshot(name):
    pin = REVISION12_SNAPSHOTS[name]
    ref = REVISION12_COMMIT + ':' + pin['path']
    raw = git('show', ref)
    if git('rev-parse', ref).decode().strip() != pin['blob'] or hashlib.sha256(raw).hexdigest() != pin['sha256'] or len(raw) != pin['bytes']:
        raise ValueError('revision12 public historical catalog identity drift: ' + name)
    return json.loads(raw)


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args])


def revision2_snapshot(name):
    pin = REVISION2_SNAPSHOTS[name]
    ref = REVISION2_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-2 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-2 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision3_snapshot(name):
    pin = REVISION3_SNAPSHOTS[name]
    ref = REVISION3_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-3 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-3 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision4_snapshot(name):
    pin = REVISION4_SNAPSHOTS[name]
    ref = REVISION4_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-4 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-4 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision5_snapshot(name):
    pin = REVISION5_SNAPSHOTS[name]
    ref = REVISION5_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-5 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-5 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision6_snapshot(name):
    pin = REVISION6_SNAPSHOTS[name]
    ref = REVISION6_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-6 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-6 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision7_snapshot(name):
    pin = REVISION7_SNAPSHOTS[name]
    ref = REVISION7_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-7 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-7 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision8_snapshot(name):
    pin = REVISION8_SNAPSHOTS[name]
    ref = REVISION8_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-8 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-8 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision8_review():
    path = str((REVISION8_EVIDENCE / 'reconciliation.json').relative_to(ROOT))
    ref = REVISION8_COMMIT + ':' + path
    if git('rev-parse', ref).decode().strip() != REVISION8_REVIEW_BLOB:
        raise ValueError('revision-8 review Git blob differs')
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != REVISION8_REVIEW_SHA256:
        raise ValueError('revision-8 complete review bytes differ')
    return json.loads(raw)


def revision9_snapshot(name):
    pin = REVISION9_SNAPSHOTS[name]
    ref = REVISION9_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-9 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-9 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision9_review():
    path = str((REVISION9_EVIDENCE / 'reconciliation.json').relative_to(ROOT))
    ref = REVISION9_COMMIT + ':' + path
    if git('rev-parse', ref).decode().strip() != REVISION9_REVIEW_BLOB:
        raise ValueError('revision-9 review Git blob differs')
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != REVISION9_REVIEW_SHA256:
        raise ValueError('revision-9 complete review bytes differ')
    return json.loads(raw)



def revision10_snapshot(name):
    pin = REVISION10_SNAPSHOTS[name]
    ref = REVISION10_COMMIT + ':' + pin['path']
    raw = git('show', ref)
    if git('rev-parse', ref).decode().strip() != pin['blob'] or hashlib.sha256(raw).hexdigest() != pin['sha256'] or len(raw) != pin['bytes']:
        raise ValueError('revision-10 historical catalog identity drift: ' + name)
    return json.loads(raw)


def revision10_review():
    pin = REVISION10_REVIEW
    ref = REVISION10_COMMIT + ':' + pin['path']
    raw = git('show', ref)
    if git('rev-parse', ref).decode().strip() != pin['blob'] or hashlib.sha256(raw).hexdigest() != pin['sha256'] or len(raw) != pin['bytes']:
        raise ValueError('revision-10 historical review identity drift')
    return json.loads(raw)


class ReviewedCatalogReconciliationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tx = json.loads(tx.CATALOG.read_text())
        cls.compat = json.loads(gate.MAP.read_text())
        cls.old_tx = json.loads((EVIDENCE / 'historical-transaction-inventory-revision1.json').read_text())
        cls.old_compat = json.loads((EVIDENCE / 'historical-compatibility-inventory-revision1.json').read_text())
        cls.revision2 = {name: revision2_snapshot(name) for name in REVISION2_SNAPSHOTS}
        cls.rev2_tx = cls.revision2['transaction']
        cls.rev2_compat = cls.revision2['compatibility']
        cls.revision3 = {name: revision3_snapshot(name) for name in REVISION3_SNAPSHOTS}
        cls.rev3_tx = cls.revision3['transaction']
        cls.rev3_compat = cls.revision3['compatibility']
        cls.revision4 = {name: revision4_snapshot(name) for name in REVISION4_SNAPSHOTS}
        cls.rev4_tx = cls.revision4['transaction']
        cls.rev4_compat = cls.revision4['compatibility']
        cls.revision5 = {name: revision5_snapshot(name) for name in REVISION5_SNAPSHOTS}
        cls.rev5_tx = cls.revision5['transaction']
        cls.rev5_compat = cls.revision5['compatibility']
        cls.revision6 = {name: revision6_snapshot(name) for name in REVISION6_SNAPSHOTS}
        cls.rev6_tx = cls.revision6['transaction']
        cls.rev6_compat = cls.revision6['compatibility']
        cls.revision7 = {name: revision7_snapshot(name) for name in REVISION7_SNAPSHOTS}
        cls.rev7_tx = cls.revision7['transaction']
        cls.rev7_compat = cls.revision7['compatibility']
        cls.revision8 = {name: revision8_snapshot(name) for name in REVISION8_SNAPSHOTS}
        cls.rev8_tx = cls.revision8['transaction']
        cls.rev8_compat = cls.revision8['compatibility']
        cls.revision9 = {name: revision9_snapshot(name) for name in REVISION9_SNAPSHOTS}
        cls.rev9_tx = cls.revision9['transaction']
        cls.rev9_compat = cls.revision9['compatibility']
        cls.revision10 = {name: revision10_snapshot(name) for name in REVISION10_SNAPSHOTS}
        cls.rev10_tx = cls.revision10['transaction']
        cls.rev10_compat = cls.revision10['compatibility']
        cls.revision11 = {name: revision11_snapshot(name) for name in REVISION11_SNAPSHOTS}
        cls.rev11_tx = cls.revision11['transaction']
        cls.rev11_compat = cls.revision11['compatibility']
        cls.revision12 = {name: revision12_snapshot(name) for name in REVISION12_SNAPSHOTS}
        cls.rev12_tx = cls.revision12['transaction']
        cls.rev12_compat = cls.revision12['compatibility']
        cls.ast = tx.extract()
        cls.migrations = tx.migration_inventory()
        cls.routes, cls.clients = gate.collect()


    @classmethod
    def frozen_revision11_root(cls):
        if '_revision11_root' not in cls.__dict__:
            temporary = tempfile.TemporaryDirectory(prefix='r5-catalog-revision11-')
            cls.addClassCleanup(temporary.cleanup)
            root = Path(temporary.name) / 'source'
            subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', '--no-checkout', str(ROOT), str(root)], check=True)
            subprocess.run(['git', '-C', str(root), 'checkout', '--quiet', '--detach', REVISION11_COMMIT], check=True)
            def frozen_git(*args):
                return subprocess.check_output(['git', '-C', str(root), *args]).decode().strip()
            if frozen_git('rev-parse', 'HEAD') != REVISION11_COMMIT or frozen_git('rev-parse', 'HEAD^{tree}') != REVISION11_TREE or frozen_git('status', '--porcelain', '--untracked-files=all'):
                raise ValueError('revision11 source checkout is not the exact clean public snapshot')
            # Reuse the installed TypeScript compiler, never current application
            # files or fabricated historical producer output.
            compiler = ROOT / 'web/node_modules/typescript'
            if not compiler.is_dir():
                raise ValueError('TypeScript required for historical original collection')
            (root / 'web/node_modules').mkdir()
            (root / 'web/node_modules/typescript').symlink_to(compiler.resolve(), target_is_directory=True)
            cls.revision11_ast = tx.extract(root)
            cls.revision11_migrations = tx.migration_inventory(root)
            with mock.patch.object(gate, 'ROOT', root):
                cls.revision11_routes, cls.revision11_clients = gate.collect()
            cls._revision11_root = root
        return cls._revision11_root

    @contextmanager
    def revision11_context(self):
        root = self.frozen_revision11_root()
        with ExitStack() as patches:
            patches.enter_context(mock.patch.dict(globals(), ROOT=root,
                CURRENT_EVIDENCE=root / 'docs/company-mail/evidence/R5-CATALOG-REVISION11-20261008',
                SOURCE_COMMIT=REVISION11_SOURCE_COMMIT, SOURCE_TREE=REVISION11_SOURCE_TREE))
            patches.enter_context(mock.patch.object(gate, 'ROOT', root))
            for name, value in dict(tx=self.rev11_tx, compat=self.rev11_compat,
                    ast=self.revision11_ast, migrations=self.revision11_migrations,
                    routes=self.revision11_routes, clients=self.revision11_clients).items():
                patches.enter_context(mock.patch.object(self, name, value))
            yield


    @classmethod
    def frozen_revision12_root(cls):
        if '_revision12_root' not in cls.__dict__:
            temporary = tempfile.TemporaryDirectory(prefix='r5-catalog-revision12-')
            cls.addClassCleanup(temporary.cleanup)
            root = Path(temporary.name) / 'source'
            subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', '--no-checkout', str(ROOT), str(root)], check=True)
            subprocess.run(['git', '-C', str(root), 'checkout', '--quiet', '--detach', REVISION12_COMMIT], check=True)
            def frozen_git(*args):
                return subprocess.check_output(['git', '-C', str(root), *args]).decode().strip()
            if frozen_git('rev-parse', 'HEAD') != REVISION12_COMMIT or frozen_git('rev-parse', 'HEAD^{tree}') != REVISION12_TREE or frozen_git('status', '--porcelain', '--untracked-files=all'):
                raise ValueError('revision12 source checkout is not the exact clean public snapshot')
            # Reuse only the installed compiler. All historical Go/TypeScript
            # inputs and original assertion bodies come from public revision12.
            compiler = ROOT / 'web/node_modules/typescript'
            if not compiler.is_dir():
                raise ValueError('TypeScript required for historical original collection')
            (root / 'web/node_modules').mkdir()
            (root / 'web/node_modules/typescript').symlink_to(compiler.resolve(), target_is_directory=True)
            cls.revision12_ast = tx.extract(root)
            cls.revision12_migrations = tx.migration_inventory(root)
            with mock.patch.object(gate, 'ROOT', root):
                cls.revision12_routes, cls.revision12_clients = gate.collect()
            if frozen_git('rev-parse', 'HEAD') != REVISION12_COMMIT or frozen_git('rev-parse', 'HEAD^{tree}') != REVISION12_TREE or frozen_git('status', '--porcelain', '--untracked-files=all'):
                raise ValueError('revision12 public checkout changed during original collection')
            cls._revision12_root = root
        return cls._revision12_root

    @contextmanager
    def revision12_context(self):
        root = self.frozen_revision12_root()
        with ExitStack() as patches:
            patches.enter_context(mock.patch.dict(globals(), ROOT=root,
                CURRENT_EVIDENCE=root / 'docs/company-mail/evidence/R5-CATALOG-REVISION12-20261008',
                SOURCE_COMMIT=REVISION12_SOURCE_COMMIT, SOURCE_TREE=REVISION12_SOURCE_TREE))
            patches.enter_context(mock.patch.object(gate, 'ROOT', root))
            for name, value in dict(tx=self.rev12_tx, compat=self.rev12_compat,
                    ast=self.revision12_ast, migrations=self.revision12_migrations,
                    routes=self.revision12_routes, clients=self.revision12_clients).items():
                patches.enter_context(mock.patch.object(self, name, value))
            yield

    def test_old_pins_reject_and_current_revision_passes_same_actual_facts(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        expected = review['current_rejections']
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(expected)).hexdigest(),
                         REVISION13_EXPECTED['review_field_sha256']['current_rejections'])
        historical = [('revision1', self.old_tx, self.old_compat),
                      ('revision2', self.rev2_tx, self.rev2_compat), ('revision3', self.rev3_tx, self.rev3_compat),
                      ('revision4', self.rev4_tx, self.rev4_compat), ('revision5', self.rev5_tx, self.rev5_compat),
                      ('revision6', self.rev6_tx, self.rev6_compat), ('revision7', self.rev7_tx, self.rev7_compat),
                      ('revision8', self.rev8_tx, self.rev8_compat), ('revision9', self.rev9_tx, self.rev9_compat),
                      ('revision10', self.rev10_tx, self.rev10_compat), ('revision11', self.rev11_tx, self.rev11_compat), ('revision12', self.rev12_tx, self.rev12_compat)]
        for revision, transaction, compatibility in historical:
            with self.assertRaises(ValueError) as rejected:
                tx.validate(transaction, self.ast, self.migrations)
            self.assertEqual(str(rejected.exception), expected['transaction'][revision])
            with self.assertRaises(ValueError) as rejected:
                gate.validate(compatibility, self.routes, self.clients)
            self.assertEqual(str(rejected.exception), expected['compatibility'][revision])
        self.assertFalse(tx.validate(self.tx, self.ast, self.migrations)['runtime_verified'])
        self.assertFalse(gate.validate(self.compat, self.routes, self.clients)['product_green'])
        for name, current in (('transaction', self.tx), ('compatibility', self.compat)):
            revision = current['inventory_revision']
            pin = REVISION12_SNAPSHOTS[name]
            self.assertEqual(revision['revision'], 13)
            self.assertEqual(revision['source_commit'], SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION12_COMMIT)
            self.assertEqual(revision['previous_snapshot'], pin['path'])
            self.assertEqual(revision['previous_snapshot_blob'], pin['blob'])
            self.assertEqual(revision['previous_sha256'], pin['sha256'])
            self.assertEqual(revision['qualification'], self.rev2_tx['inventory_revision']['qualification'])

    def test_only_reviewed_body_and_two_closure_hashes_change(self):
        old = {e['id']: e for e in self.old_tx['entries']}
        current = {e['id']: e for e in self.rev2_tx['entries']}
        self.assertEqual(set(old), set(current))
        self.assertEqual([k for k in old if old[k]['syntax']['sha256'] != current[k]['syntax']['sha256']],
                         ['internal/store/postgres/employee_disposition.go::offboardingSubjectsTx'])
        self.assertEqual(self.old_tx['migrations'], self.rev2_tx['migrations'])
        self.assertEqual(self.old_tx['historical_review_metadata'], self.rev2_tx['historical_review_metadata'])
        self.assertEqual(self.old_tx['reviewed_file_types'], self.rev2_tx['reviewed_file_types'])
        for key in old:
            self.assertEqual(old[key]['classification'], current[key]['classification'])
            self.assertEqual(old[key]['evidence_level'], current[key]['evidence_level'])
        self.assertEqual(set(self.old_compat['source_closure']), set(self.rev2_compat['source_closure']))
        changed = {k for k in self.rev2_compat['source_closure'] if self.rev2_compat['source_closure'][k] != self.old_compat['source_closure'][k]}
        self.assertEqual(changed, {'internal/api/handlers/r5_protocol_component_observations_test.go', 'internal/store/postgres/r5_protocol_shared_test.go'})
        for key in self.old_compat:
            if key != 'source_closure':
                self.assertEqual(self.old_compat[key], self.rev2_compat[key])
        for historical in (self.rev2_tx, self.rev2_compat):
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 2)
            self.assertEqual(hashlib.sha256((ROOT / revision['previous_snapshot']).read_bytes()).hexdigest(), revision['previous_sha256'])

    def test_revision3_preserves_unmodified_reviews_and_pins_reviewed_sources(self):
        old_entries = {entry['id']: entry for entry in self.rev2_tx['entries']}
        current_entries = {entry['id']: entry for entry in self.rev3_tx['entries']}
        self.assertEqual(set(old_entries), set(current_entries))
        changed = set()
        sources = set()
        for name, before in old_entries.items():
            after = current_entries[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            self.assertEqual(len(before['callers']), len(after['callers']))
            for old, now in zip(before['callers'], after['callers']):
                self.assertEqual({k: v for k, v in old.items() if k != 'line'},
                                 {k: v for k, v in now.items() if k != 'line'})
                if old != now:
                    changed.add(name)
                    sources.add(now['file'])
        self.assertEqual(changed, {tx.PG + suffix for suffix in (
            'outbound_retry_reader.go:*outboundRetryReader:GetUser', 'postgres.go:*PgStore:Close', 'postgres.go::New',
            'refresh_rotation.go:*PgStore:RevokeRefreshTokenByHash', 'refresh_rotation.go:*PgStore:RotateRefreshToken',
            'tenants.go:*PgStore:CreateTenant', 'users.go:*PgStore:ChangePasswordAtomic', 'users.go:*PgStore:CreateRefreshToken',
            'users.go:*PgStore:CreateUser', 'users.go:*PgStore:GetUser', 'users.go:*PgStore:GetUserByEmail',
            'users.go:*PgStore:RevokeUserRefreshTokens', 'users.go:*PgStore:TouchUserLogin', 'users.go:*PgStore:UpdateUserPassword')})
        self.assertEqual(sources, {'internal/api/handlers/auth.go', 'internal/outbound/builder.go', 'internal/outbound/delivery.go'})
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev2_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev3_tx.items() if k not in mutable})
        self.assertEqual(self.rev3_tx['baseline_commit'], REVISION3_SOURCE_COMMIT)
        self.assertEqual(self.rev3_tx['last_review_base_commit'], REVISION3_SOURCE_COMMIT)

        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev2_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev3_compat.items() if k not in mutable})
        self.assertEqual(self.rev3_compat['acquisition'], {**self.rev2_compat['acquisition'], 'base_commit': REVISION3_SOURCE_COMMIT})
        self.assertEqual(len(self.rev2_compat['routes']), len(self.rev3_compat['routes']))
        for before, after in zip(self.rev2_compat['routes'], self.rev3_compat['routes']):
            before = {k: v for k, v in before.items() if k != 'clients'}
            after = {k: v for k, v in after.items() if k != 'clients'}
            if before['route'] == 'POST /api/v1/auth/refresh':
                before = copy.deepcopy(before)
                self.assertTrue(before['request_contract']['required'])
                self.assertEqual(before['request_contract']['media_type_schemas']['application/json']['required'], ['refresh_token'])
                before['request_contract']['required'] = False
                del before['request_contract']['media_type_schemas']['application/json']['required']
            self.assertEqual(before, after)

        closure_changes = {
            str(gate.CURRENT_CLIENTS), 'internal/api/handlers/auth.go',
            'internal/api/handlers/r5_protocol_component_observations_test.go', 'internal/api/openapi.yaml',
            'web/components/company/compose.tsx', 'web/features/mail/components/draft-folder.tsx',
            'web/features/mail/components/message-pane.tsx', 'web/features/mail/components/submission-content.tsx',
            'web/features/mail/workspace.tsx', 'web/lib/api/base.ts',
        }
        self.assertEqual(set(self.rev2_compat['source_closure']), set(self.rev3_compat['source_closure']))
        self.assertEqual({p for p, sha in self.rev3_compat['source_closure'].items() if self.rev2_compat['source_closure'][p] != sha}, closure_changes)
        review = json.loads((REVISION3_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['source_commit'], REVISION3_SOURCE_COMMIT)
        self.assertEqual(review['revision2_snapshots'], {name: dict(commit=REVISION2_COMMIT, **pin) for name, pin in REVISION2_SNAPSHOTS.items()})
        reviewed_paths = {row['path'] for row in review['source_changes']}
        self.assertEqual(len(review['source_changes']), len(reviewed_paths))
        self.assertEqual(reviewed_paths, (closure_changes - {str(gate.CURRENT_CLIENTS)}) | sources)
        for row in review['source_changes']:
            path = row['path']
            for prefix, commit in [('before', REVISION2_COMMIT), ('after', REVISION3_SOURCE_COMMIT)]:
                raw = git('show', commit + ':' + path)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
                self.assertEqual(git('rev-parse', commit + ':' + path).decode().strip(), row[prefix + '_blob'])
            self.assertEqual(git('show', REVISION3_COMMIT + ':' + path), git('show', REVISION3_SOURCE_COMMIT + ':' + path))
        original = review['unchanged_validators_and_collectors']
        self.assertEqual(set(original), {'scripts/check_r5_transactions.py', 'scripts/check_r5_compatibility.py',
            'scripts/collect_api_calls.cjs', 'cmd/r5txinventory/main.go', 'internal/architecture/route_inventory_test.go'})
        for path, sha in original.items():
            raw = git('show', REVISION2_COMMIT + ':' + path)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), sha)

    def test_revision3_client_inventory_preserves_routes_and_forwarders(self):
        documented = self.revision3['client_routes']
        self.assertEqual(len(self.revision3['clients']), 134)
        self.assertEqual(len(documented), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.revision3['clients']), 7)
        line_changes = 0
        owner_changes = []
        for old, now, previous_doc, current_doc in zip(self.revision2['clients'], self.revision3['clients'], self.revision2['client_routes'], documented):
            self.assertEqual({k: v for k, v in current_doc.items() if k != 'routes'}, now)
            self.assertEqual(previous_doc['routes'], current_doc['routes'])
            line_changes += old['line'] != now['line']
            old = {k: v for k, v in old.items() if k != 'line'}
            now = {k: v for k, v in now.items() if k != 'line'}
            if old['owner'] != now['owner']:
                owner_changes.append((old['source'], old['path'], old['methods'], old['owner'], now['owner']))
                old['owner'] = now['owner']
            self.assertEqual(old, now)
        self.assertEqual(line_changes, 19)
        self.assertEqual(owner_changes, [('web/components/company/compose.tsx', '/api/v1/company/mailboxes/{id}/attachments', ['POST'], 'a', 'Compose')])

    def test_revision4_preserves_reviews_and_records_exact_caller_changes(self):
        review = json.loads(git('show', REVISION4_COMMIT + ':' + str((REVISION4_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['source_commit'], REVISION4_SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], git('rev-parse', REVISION4_SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision3_snapshots'], {name: dict(commit=REVISION3_COMMIT, **pin) for name, pin in REVISION3_SNAPSHOTS.items()})
        for name, historical in [('transaction', self.rev3_tx), ('compatibility', self.rev3_compat)]:
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 3)
            self.assertEqual(revision['source_commit'], REVISION3_SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION2_COMMIT)
            for field, pin_field in [('previous_snapshot', 'path'), ('previous_snapshot_blob', 'blob'), ('previous_sha256', 'sha256')]:
                self.assertEqual(revision[field], REVISION2_SNAPSHOTS[name][pin_field])
        old = {entry['id']: entry for entry in self.rev3_tx['entries']}
        current = {entry['id']: entry for entry in self.rev4_tx['entries']}
        self.assertEqual(set(old), set(current))
        changes = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(changes), len(review['transaction_callers']))
        self.assertEqual(set(changes), {tx.PG + suffix for suffix in (
            'company_mail.go:*PgStore:FinishMailAttachment', 'company_mail.go:*PgStore:GetWorkAttachment',
            'company_mail.go:*PgStore:ReserveMailAttachment', 'company_members.go:*PgStore:GetWorkMailbox',
            'outbound.go:*PgStore:CreateOutboundAttempt', 'outbound.go:*PgStore:IsSuppressed',
            'outbound.go:*PgStore:MarkOutboundJobFailed', 'outbound.go:*PgStore:MarkOutboundJobSent',
            'outbound_recipients.go:*PgStore:BeginOutboundRecipient', 'outbound_recipients.go:*PgStore:CompleteOutboundRecipient',
            'postgres.go:*PgStore:Close', 'postgres.go::New', 'submissions.go:*PgStore:GetSubmissionAttachment',
            'submissions.go:*PgStore:GetSubmissionContent', 'submissions.go:*PgStore:ListSubmissionAttachments')})
        additions, removals = [], []
        locations = 0
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in changes:
                self.assertEqual(before['callers'], after['callers'])
                continue
            delta = changes[name]
            for prefix, value in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(delta[prefix + '_count'], len(value))
                self.assertEqual(delta[prefix + '_sha256'], hashlib.sha256(gate.canonical_bytes(value)).hexdigest())
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
                removals.append((name, row['caller_id'], row['expression']))
            for row in delta['locations']:
                matches = [c for c in transformed if c['caller_id'] == row['caller_id'] and c['file'] == row['source']
                           and c['expression'] == row['expression'] and c['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                matches[0]['line'] = row['after_line']
                locations += 1
            for row in delta['added']:
                transformed.append(row)
                additions.append((name, row['caller_id'], row['expression']))
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        self.assertEqual(locations, 29)
        self.assertEqual(additions, [(tx.PG + 'postgres.go:*PgStore:Close', 'internal/app/companymail/attachment_read.go::readOwnedAttachment', 'input.Close')])
        self.assertEqual(removals, [(tx.PG + 'postgres.go:*PgStore:Close', 'internal/app/companymail/service.go:*Service:verifiedFile', 'r.Close'),
                                    (tx.PG + 'postgres.go::New', 'internal/mailcontent/parser.go:*Parser:Attachment', 'errors.New')])
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev3_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev4_tx.items() if k not in mutable})
        self.assertEqual(self.rev4_tx['baseline_commit'], REVISION4_SOURCE_COMMIT)
        self.assertEqual(self.rev4_tx['last_review_base_commit'], REVISION4_SOURCE_COMMIT)

    def test_revision4_client_locations_and_source_hashes_are_explicitly_bound(self):
        review = json.loads(git('show', REVISION4_COMMIT + ':' + str((REVISION4_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        documented = self.revision4['client_routes']
        self.assertEqual(len(self.revision4['clients']), 134)
        self.assertEqual(len(documented), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.revision4['clients']), 7)
        changed_indices = []
        for index, (before, after, previous_doc, current_doc) in enumerate(zip(self.revision3['clients'], self.revision4['clients'], self.revision3['client_routes'], documented)):
            self.assertEqual({k: v for k, v in before.items() if k != 'line'}, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual(previous_doc['routes'], current_doc['routes'])
            self.assertEqual({k: v for k, v in current_doc.items() if k != 'routes'}, after)
            if before != after:
                changed_indices.append(index)
        self.assertEqual(changed_indices, [0, 1, 2, 3, 4, 5, 93, 94, 95])
        self.assertEqual([row['index'] for row in review['client_locations']], changed_indices)
        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev3_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev4_compat.items() if k not in mutable})
        self.assertEqual(self.rev4_compat['acquisition'], dict(self.rev3_compat['acquisition'], base_commit=REVISION4_SOURCE_COMMIT))
        self.assertEqual(len(self.rev3_compat['routes']), len(self.rev4_compat['routes']))
        changed_routes = []
        for before, after in zip(self.rev3_compat['routes'], self.rev4_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                changed_routes.append({'route': after['route'], 'changed_fields': ['clients']})
        self.assertEqual(len(changed_routes), 7)
        self.assertEqual(review['compatibility_routes'], changed_routes)
        closure_changes = {str(gate.CURRENT_CLIENTS), 'web/app/(dashboard)/company/recovery/page.tsx', 'web/lib/api/base.ts'}
        self.assertEqual(set(self.rev3_compat['source_closure']), set(self.rev4_compat['source_closure']))
        self.assertEqual({p for p, sha in self.rev4_compat['source_closure'].items() if self.rev3_compat['source_closure'][p] != sha}, closure_changes)
        paths = (closure_changes - {str(gate.CURRENT_CLIENTS)}) | {
            'internal/app/companymail/attachment_read.go', 'internal/app/companymail/service.go', 'internal/mailcontent/parser.go',
            'internal/outbound/delivery.go', 'internal/outbound/recipient_delivery.go'}
        self.assertEqual({row['path'] for row in review['source_changes']}, paths)
        self.assertEqual(len(review['source_changes']), len(paths))
        for row in review['source_changes']:
            path = row['path']
            if path == 'internal/app/companymail/attachment_read.go':
                self.assertEqual(git('ls-tree', REVISION3_COMMIT, '--', path), b'')
                self.assertIsNone(row['before_sha256'])
                self.assertIsNone(row['before_blob'])
            else:
                self.assertEqual(hashlib.sha256(git('show', REVISION3_COMMIT + ':' + path)).hexdigest(), row['before_sha256'])
                self.assertEqual(git('rev-parse', REVISION3_COMMIT + ':' + path).decode().strip(), row['before_blob'])
            raw = git('show', REVISION4_SOURCE_COMMIT + ':' + path)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), row['after_sha256'])
            self.assertEqual(git('rev-parse', REVISION4_SOURCE_COMMIT + ':' + path).decode().strip(), row['after_blob'])
            self.assertEqual(git('show', REVISION4_COMMIT + ':' + path), raw)
        historical_review = json.loads((REVISION3_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['unchanged_validators_and_collectors'], historical_review['unchanged_validators_and_collectors'])

    def test_revision5_preserves_reviews_and_records_exact_caller_changes(self):
        review = json.loads(git('show', REVISION5_COMMIT + ':' + str((REVISION5_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['inventory_revision'], 5)
        self.assertEqual(review['source_commit'], REVISION5_SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], '1621d5fefb443d9d314abc2f9e0fab94914ce4c4')
        self.assertEqual(review['source_tree'], git('rev-parse', REVISION5_SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision4_snapshots'], {name: dict(commit=REVISION4_COMMIT, **pin) for name, pin in REVISION4_SNAPSHOTS.items()})
        for name, historical in [('transaction', self.rev4_tx), ('compatibility', self.rev4_compat)]:
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 4)
            self.assertEqual(revision['source_commit'], REVISION4_SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION3_COMMIT)
            for field, pin_field in [('previous_snapshot', 'path'), ('previous_snapshot_blob', 'blob'), ('previous_sha256', 'sha256')]:
                self.assertEqual(revision[field], REVISION3_SNAPSHOTS[name][pin_field])

        old = {entry['id']: entry for entry in self.rev4_tx['entries']}
        current = {entry['id']: entry for entry in self.rev5_tx['entries']}
        self.assertEqual(set(old), set(current))
        changes = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(changes), len(review['transaction_callers']))
        self.assertEqual(set(changes), {tx.PG + suffix for suffix in (
            'company_mail.go:*PgStore:OutboundAttachments', 'outbound.go:*PgStore:IsSuppressed',
            'outbound_content.go:*PgStore:CanReadOutboundContent', 'outbound_retry.go:*PgStore:RequeueOutboundJobAuthorized',
            'postgres.go:*PgStore:Close', 'postgres.go::New')})
        additions, removals, locations = [], [], 0
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in changes:
                self.assertEqual(before['callers'], after['callers'])
                continue
            delta = changes[name]
            for prefix, value in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(delta[prefix + '_count'], len(value))
                self.assertEqual(delta[prefix + '_sha256'], hashlib.sha256(gate.canonical_bytes(value)).hexdigest())
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
                removals.append((name, row['caller_id'], row['expression']))
            for row in delta['locations']:
                matches = [c for c in transformed if c['caller_id'] == row['caller_id'] and c['file'] == row['source']
                           and c['expression'] == row['expression'] and c['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                matches[0]['line'] = row['after_line']
                locations += 1
            for row in delta['added']:
                transformed.append(row)
                additions.append((name, row['caller_id'], row['expression']))
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        self.assertEqual(locations, 8)
        self.assertEqual(additions, [
            (tx.PG + 'postgres.go:*PgStore:Close', 'internal/outbound/queued_attachment_read.go::readQueuedAttachment', 'input.Close'),
            (tx.PG + 'postgres.go::New', 'internal/app/recovery/verify.go::Verify', 'sha256.New'),
            (tx.PG + 'postgres.go::New', 'internal/outbound/queued_attachment_read.go::readQueuedAttachment', 'errors.New')])
        self.assertEqual(removals, [])
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev4_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev5_tx.items() if k not in mutable})
        self.assertEqual(self.rev5_tx['baseline_commit'], REVISION5_SOURCE_COMMIT)
        self.assertEqual(self.rev5_tx['last_review_base_commit'], REVISION5_SOURCE_COMMIT)
        self.assertFalse(review['transaction']['task_complete'])
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['compatibility']['task_complete'])
        self.assertFalse(review['compatibility']['product_green'])

    def test_revision5_client_moves_and_source_hashes_are_explicitly_bound(self):
        review = json.loads(git('show', REVISION5_COMMIT + ':' + str((REVISION5_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        documented = self.revision5['client_routes']
        self.assertEqual(len(self.revision5['clients']), 134)
        self.assertEqual(len(documented), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.revision5['clients']), 7)
        # Publish moves from the page into save; preview moves ahead of save in
        # the editor. Preserve the original route identities through both moves.
        previous_indices = list(range(8)) + list(range(9, 21)) + [23, 21, 22, 8] + list(range(24, 134))
        self.assertEqual(sorted(previous_indices), list(range(134)))
        client_changes, reordered = [], []
        for index, previous_index in enumerate(previous_indices):
            before, after = self.revision4['clients'][previous_index], self.revision5['clients'][index]
            expected = {k: v for k, v in before.items() if k != 'line'}
            if previous_index == 8:
                expected.update(source='web/components/company/templates/editor.tsx', owner='save',
                                expression='`/templates/${res.id}/publish`')
            if previous_index == 21:
                expected['expression'] = '`/templates/${snapshot.id}`'
            if previous_index == 23:
                expected['owner'] = 'value'
            self.assertEqual(expected, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual(self.revision4['client_routes'][previous_index]['routes'], documented[index]['routes'])
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            if before != after:
                client_changes.append(dict(before_index=previous_index, after_index=index, path=after['path'], methods=after['methods'],
                    changes={k: dict(before=before[k], after=after[k]) for k in before if before[k] != after[k]}))
            if previous_index != index:
                reordered.append(dict(before_index=previous_index, after_index=index))
        self.assertEqual([(row['before_index'], row['after_index']) for row in client_changes],
                         [(6, 6), (7, 7), (23, 20), (21, 21), (22, 22), (8, 23), (27, 27)])
        self.assertEqual(review['client_changes'], client_changes)
        self.assertEqual(review['client_reorder'], reordered)
        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev4_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev5_compat.items() if k not in mutable})
        self.assertEqual(self.rev5_compat['acquisition'], dict(self.rev4_compat['acquisition'], base_commit=REVISION5_SOURCE_COMMIT))
        self.assertEqual(len(self.rev4_compat['routes']), len(self.rev5_compat['routes']))
        changed_routes = []
        for before, after in zip(self.rev4_compat['routes'], self.rev5_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                changed_routes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual({row['route'] for row in changed_routes}, {
            'GET /api/v1/company/templates', 'POST /api/v1/company/templates', 'POST /api/v1/company/templates/preview',
            'POST /api/v1/company/templates/{id}/publish', 'POST /api/v1/company/templates/{id}/retire',
            'PUT /api/v1/company/templates/{id}', 'GET /api/v1/company/templates/{id}/versions'})
        self.assertEqual(review['compatibility_routes'], changed_routes)
        closure_changes = {str(gate.CURRENT_CLIENTS), 'internal/company/templates.go',
            'web/app/(dashboard)/company/templates/page.tsx', 'web/components/company/templates/editor.tsx',
            'web/components/company/templates/versions.tsx'}
        self.assertEqual(set(self.rev4_compat['source_closure']), set(self.rev5_compat['source_closure']))
        self.assertEqual({p for p, sha in self.rev5_compat['source_closure'].items() if self.rev4_compat['source_closure'][p] != sha}, closure_changes)
        paths = (closure_changes - {str(gate.CURRENT_CLIENTS)}) | {
            'internal/app/recovery/verify.go', 'internal/app/submissions/service.go',
            'internal/outbound/company.go', 'internal/outbound/queued_attachment_read.go'}
        self.assertEqual({row['path'] for row in review['source_changes']}, paths)
        self.assertEqual(len(review['source_changes']), len(paths))
        for row in review['source_changes']:
            path = row['path']
            if path == 'internal/outbound/queued_attachment_read.go':
                self.assertEqual(git('ls-tree', REVISION4_COMMIT, '--', path), b'')
                self.assertIsNone(row['before_blob'])
                self.assertIsNone(row['before_sha256'])
            else:
                self.assertEqual(git('rev-parse', REVISION4_COMMIT + ':' + path).decode().strip(), row['before_blob'])
                self.assertEqual(hashlib.sha256(git('show', REVISION4_COMMIT + ':' + path)).hexdigest(), row['before_sha256'])
            raw = git('show', REVISION5_SOURCE_COMMIT + ':' + path)
            self.assertEqual(git('rev-parse', REVISION5_SOURCE_COMMIT + ':' + path).decode().strip(), row['after_blob'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), row['after_sha256'])
            self.assertEqual(git('show', REVISION5_COMMIT + ':' + path), raw)
        self.assertEqual(set(review['generated_catalogs']), set(REVISION4_SNAPSHOTS))
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION4_SNAPSHOTS[name]['path'])
            self.assertEqual(hashlib.sha256(git('show', REVISION5_COMMIT + ':' + value['path'])).hexdigest(), value['sha256'])
        previous_review = json.loads(git('show', REVISION4_COMMIT + ':' + str((REVISION4_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['unchanged_validators_and_collectors'], previous_review['unchanged_validators_and_collectors'])
        # All existing historical packets remain byte-for-byte pinned, including
        # the revision-4 review now read directly from its public Git object.
        for directory in (EVIDENCE, REVISION3_EVIDENCE, REVISION4_EVIDENCE):
            names = git('ls-tree', '-r', '--name-only', REVISION4_COMMIT, '--', str(directory.relative_to(ROOT))).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(p.relative_to(ROOT)) for p in directory.rglob('*') if p.is_file()}, set(names))
            for path in names:
                self.assertEqual((ROOT / path).read_bytes(), git('show', REVISION4_COMMIT + ':' + path))

    def test_unapproved_client_and_caller_locations_are_rejected(self):
        clients = copy.deepcopy(self.clients)
        clients[0]['line'] += 1
        with self.assertRaisesRegex(ValueError, '^client producer differs'):
            gate.validate(self.compat, self.routes, clients)
        ast = copy.deepcopy(self.ast)
        caller = next(f for f in ast['functions'] if f['file'] == 'internal/api/handlers/auth.go' and f['name'] == 'Login')
        next(call for call in caller['calls'] if call['name'] == 'GetUserByEmail')['line'] += 1
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.tx, ast, self.migrations)

    def test_unapproved_actual_source_and_ast_changes_rejected(self):
        # Copy only real producer inputs; no product binary, service or source edit.
        with tempfile.TemporaryDirectory(prefix='r5-catalog-negative-') as directory:
            root = Path(directory)
            for group in ('internal', 'cmd'):
                for source in (ROOT / group).rglob('*.go'):
                    if source.name.endswith('_test.go') or source.is_relative_to(ROOT / 'cmd/r5txinventory'):
                        continue
                    target = root / source.relative_to(ROOT)
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(source, target)
            self.assertEqual(tx.extract(root), self.ast)
            path = root / 'internal/store/postgres/employee_disposition.go'
            source = path.read_text()
            self.assertEqual(source.count('successor == a.ID ||'), 1)
            path.write_text(source.replace('successor == a.ID ||', ''))
            with self.assertRaisesRegex(ValueError, 'function syntax drift:.*offboardingSubjectsTx'):
                tx.validate(self.tx, tx.extract(root), self.migrations)
            path.write_text(source + '\n// unreviewed file-only delta\n')
            with self.assertRaisesRegex(ValueError, 'postgres file set/content drift'):
                tx.validate(self.tx, tx.extract(root), self.migrations)

    def test_unapproved_actual_closure_bytes_rejected(self):
        # Clone the exact closure, alter actual bytes, and use the real hasher.
        with tempfile.TemporaryDirectory(prefix='r5-closure-negative-') as directory:
            root = Path(directory)
            for relative in self.compat['source_closure']:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, path)
            path = root / 'internal/store/postgres/r5_protocol_shared_test.go'
            path.write_bytes(path.read_bytes() + b'\n// unreviewed closure delta\n')
            actual_facts = gate.source_facts(self.routes, self.clients)
            with mock.patch.object(gate, 'ROOT', root):
                changed = gate.closure(actual_facts, self.clients)
            # Only root location is substituted; all digests derive from real bytes.
            with mock.patch.object(gate, 'closure', return_value=changed):
                with self.assertRaisesRegex(ValueError, '^source hash drift$'):
                    gate.validate(self.compat, self.routes, self.clients)

    def test_unapproved_route_and_ast_fact_changes_rejected(self):
        routes = copy.deepcopy(self.routes)
        routes[0]['handler'] = 'unapproved.handler'
        with self.assertRaisesRegex(ValueError, 'route producer differs'):
            gate.validate(self.compat, routes, self.clients)
        ast = copy.deepcopy(self.ast)
        function = next(f for f in ast['functions'] if f['name'] == 'offboardingSubjectsTx')
        function['calls'].append({'line': function['end'], 'expr': 'tx.Exec', 'name': 'Exec', 'sql_expr': '`DELETE FROM users`'})
        with self.assertRaisesRegex(ValueError, 'function syntax drift:.*offboardingSubjectsTx'):
            tx.validate(self.tx, ast, self.migrations)

    def test_unapproved_actual_route_source_rejected_by_original_collector(self):
        with tempfile.TemporaryDirectory(prefix='r5-route-source-negative-') as directory:
            root = Path(directory)
            for name in ('go.mod', 'go.sum', 'internal/architecture/route_inventory_test.go',
                         'internal/api/router.go', 'internal/api/handlers/company_routes.go',
                         'docs/company-mail/evidence/R5-API-MATRIX.json'):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / name, path)
            go, env = r5_go_environment.selected()
            env['GOPROXY'] = 'off'
            argv = [go, 'test', '-mod=readonly', '-count=1', './internal/architecture', '-run', '^TestR5RouteInventory$']
            before = subprocess.run(argv, cwd=root, env=env, capture_output=True, text=True, timeout=60)
            self.assertEqual(before.returncode, 0, before.stdout + before.stderr)
            path = root / 'internal/api/router.go'
            source = path.read_text()
            self.assertEqual(source.count('"/docs-assets/*"'), 1)
            path.write_text(source.replace('"/docs-assets/*"', '"/unreviewed-docs-assets/*"'))
            after = subprocess.run(argv, cwd=root, env=env, capture_output=True, text=True, timeout=60)
            self.assertNotEqual(after.returncode, 0)
            self.assertIn('undocumented route GET /unreviewed-docs-assets/*', after.stdout)


    def test_revision6_preserves_reviews_and_records_exact_caller_changes(self):
        review = json.loads(git('show', REVISION6_COMMIT + ':' + str((REVISION6_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['inventory_revision'], 6)
        self.assertEqual(review['source_commit'], REVISION6_SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], REVISION6_SOURCE_TREE)
        self.assertEqual(REVISION6_SOURCE_TREE, git('rev-parse', REVISION6_SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision5_snapshots'],
                         {name: dict(commit=REVISION5_COMMIT, **pin) for name, pin in REVISION5_SNAPSHOTS.items()})
        for name, historical in [('transaction', self.rev5_tx), ('compatibility', self.rev5_compat)]:
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 5)
            self.assertEqual(revision['source_commit'], REVISION5_SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION4_COMMIT)
            for field, pin_field in [('previous_snapshot', 'path'), ('previous_snapshot_blob', 'blob'), ('previous_sha256', 'sha256')]:
                self.assertEqual(revision[field], REVISION4_SNAPSHOTS[name][pin_field])

        old = {entry['id']: entry for entry in self.rev5_tx['entries']}
        current = {entry['id']: entry for entry in self.rev6_tx['entries']}
        self.assertEqual(set(old), set(current))
        changes = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(changes), len(review['transaction_callers']))
        self.assertEqual(sorted(changes), REVISION6_EXPECTED['caller_ids'])
        additions, removals, locations = [], [], []
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in changes:
                self.assertEqual(before['callers'], after['callers'])
                continue
            delta = changes[name]
            for prefix, value in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(delta[prefix + '_count'], len(value))
                self.assertEqual(delta[prefix + '_sha256'], hashlib.sha256(gate.canonical_bytes(value)).hexdigest())
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
                removals.append((name, row['caller_id'], row['expression']))
            for row in delta['locations']:
                matches = [c for c in transformed if c['caller_id'] == row['caller_id'] and c['file'] == row['source']
                           and c['expression'] == row['expression'] and c['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                self.assertNotEqual(row['before_line'], row['after_line'])
                matches[0]['line'] = row['after_line']
                locations.append(dict(id=name, **row))
            for row in delta['added']:
                transformed.append(row)
                additions.append((name, row['caller_id'], row['expression']))
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        self.assertEqual(locations, REVISION6_EXPECTED['caller_locations'])
        self.assertEqual(additions, REVISION6_EXPECTED['callers_added'])
        self.assertEqual(removals, REVISION6_EXPECTED['callers_removed'])
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev5_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev6_tx.items() if k not in mutable})
        self.assertEqual(self.rev6_tx['baseline_commit'], REVISION6_SOURCE_COMMIT)
        self.assertEqual(self.rev6_tx['last_review_base_commit'], REVISION6_SOURCE_COMMIT)
        self.assertFalse(review['transaction']['task_complete'])
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['compatibility']['task_complete'])
        self.assertFalse(review['compatibility']['product_green'])

    def test_revision6_client_facts_source_bytes_and_historical_guards_are_bound(self):
        review = json.loads(git('show', REVISION6_COMMIT + ':' + str((REVISION6_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        documented = self.revision6['client_routes']
        self.assertEqual(len(self.revision6['clients']), REVISION6_EXPECTED['client_branch_count'])
        self.assertEqual(len(documented), REVISION6_EXPECTED['client_branch_count'])
        self.assertEqual(sum(row['forwarding'] for row in self.revision6['clients']), 7)
        indices = REVISION6_EXPECTED['client_previous_indices']
        groups = {index: [previous] for index, previous in enumerate(indices)}
        self.assertEqual(review['client_merges'], REVISION6_EXPECTED['client_merges'])
        for merge in REVISION6_EXPECTED['client_merges']:
            index, previous = merge['after_index'], merge['before_indices']
            self.assertEqual(indices[index], previous[0])
            self.assertGreaterEqual(len(previous), 2)
            for old_index in previous[1:]:
                self.assertEqual({k: v for k, v in self.revision5['clients'][old_index].items() if k != 'line'},
                                 {k: v for k, v in self.revision5['clients'][previous[0]].items() if k != 'line'})
                self.assertEqual(self.revision5['client_routes'][old_index]['routes'], documented[index]['routes'])
            groups[index] = previous
        self.assertEqual(sorted(previous for group in groups.values() for previous in group), list(range(134)))
        self.assertEqual(review['client_previous_indices'], indices)
        changes, reordered = [], []
        for index, previous in enumerate(indices):
            before, after = self.revision5['clients'][previous], self.revision6['clients'][index]
            expected = {k: v for k, v in before.items() if k != 'line'}
            expected.update(REVISION6_EXPECTED['client_overrides'].get(index, {}))
            self.assertEqual(expected, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual(self.revision5['client_routes'][previous]['routes'], documented[index]['routes'])
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            if before != after:
                changes.append(dict(before_index=previous, after_index=index, path=after['path'], methods=after['methods'],
                    changes={k: dict(before=before[k], after=after[k]) for k in before if before[k] != after[k]}))
            if previous != index:
                reordered.append(dict(before_index=previous, after_index=index))
        self.assertEqual([(row['before_index'], row['after_index']) for row in changes], REVISION6_EXPECTED['client_changed_indices'])
        self.assertEqual(review['client_changes'], changes)
        self.assertEqual(review['client_reorder'], reordered)
        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev5_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev6_compat.items() if k not in mutable})
        self.assertEqual(self.rev6_compat['acquisition'], dict(self.rev5_compat['acquisition'], base_commit=REVISION6_SOURCE_COMMIT))
        self.assertEqual(len(self.rev5_compat['routes']), len(self.rev6_compat['routes']))
        changed_routes = []
        for before, after in zip(self.rev5_compat['routes'], self.rev6_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                changed_routes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual(sorted(row['route'] for row in changed_routes), REVISION6_EXPECTED['changed_routes'])
        self.assertEqual(review['compatibility_routes'], changed_routes)
        self.assertEqual(len(self.rev6_compat['source_closure']), 94)
        self.assertEqual(set(self.rev5_compat['source_closure']), set(self.rev6_compat['source_closure']))
        changed = sorted(path for path, digest in self.rev6_compat['source_closure'].items()
                         if digest != self.rev5_compat['source_closure'][path])
        self.assertEqual(changed, REVISION6_EXPECTED['closure_paths'])
        self.assertEqual(review['closure_changes'], changed)
        self.assertEqual([row['path'] for row in review['source_changes']], REVISION6_EXPECTED['source_paths'])
        for row in review['source_changes']:
            path = row['path']
            if git('ls-tree', REVISION5_COMMIT, '--', path):
                before = git('show', REVISION5_COMMIT + ':' + path)
                self.assertEqual(git('rev-parse', REVISION5_COMMIT + ':' + path).decode().strip(), row['before_blob'])
                self.assertEqual(hashlib.sha256(before).hexdigest(), row['before_sha256'])
            else:
                before = None
                self.assertIsNone(row['before_blob'])
                self.assertIsNone(row['before_sha256'])
            raw = git('show', REVISION6_SOURCE_COMMIT + ':' + path)
            self.assertNotEqual(before, raw)
            self.assertEqual(git('rev-parse', REVISION6_SOURCE_COMMIT + ':' + path).decode().strip(), row['after_blob'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), row['after_sha256'])
            self.assertEqual(git('show', REVISION6_SOURCE_COMMIT + ':' + path), raw)
        self.assertEqual(set(review['generated_catalogs']), set(REVISION5_SNAPSHOTS))
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION5_SNAPSHOTS[name]['path'])
            self.assertEqual(hashlib.sha256(git('show', REVISION6_COMMIT + ':' + value['path'])).hexdigest(), value['sha256'])
        previous_review = json.loads(git('show', REVISION5_COMMIT + ':' + str((REVISION5_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['unchanged_validators_and_collectors'], previous_review['unchanged_validators_and_collectors'])
        self.assertEqual(len(review['unchanged_validators_and_collectors']), 5)
        for path, digest in review['unchanged_validators_and_collectors'].items():
            raw = git('show', REVISION5_COMMIT + ':' + path)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
            self.assertEqual(git('show', REVISION6_SOURCE_COMMIT + ':' + path), raw)
        historical_files = 0
        for directory in (EVIDENCE, REVISION3_EVIDENCE, REVISION4_EVIDENCE, REVISION5_EVIDENCE):
            names = git('ls-tree', '-r', '--name-only', REVISION5_COMMIT, '--', str(directory.relative_to(ROOT))).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(p.relative_to(ROOT)) for p in directory.rglob('*') if p.is_file()}, set(names))
            historical_files += len(names)
            for path in names:
                self.assertEqual((ROOT / path).read_bytes(), git('show', REVISION5_COMMIT + ':' + path))
        self.assertEqual(historical_files, 33)
        test_path = 'scripts/tests/test_r5_catalog_reconciliation.py'
        def methods(raw):
            return {node.name: ast.dump(node, include_attributes=False) for node in ast.walk(ast.parse(raw))
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        before, after = methods(git('show', REVISION5_COMMIT + ':' + test_path)), methods(git('show', REVISION6_COMMIT + ':' + test_path))
        self.assertEqual(len(before), 13)
        self.assertEqual(len(after), 15)
        self.assertTrue(set(before).issubset(after))
        mutations = {name for name in before if name.startswith('test_unapproved_')}
        self.assertEqual(len(mutations), 5)
        for name in mutations:
            self.assertEqual(before[name], after[name])



    def test_revision7_preserves_reviews_and_records_exact_caller_changes(self):
        review = json.loads(git('show', REVISION7_COMMIT + ':' + str((REVISION7_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['inventory_revision'], 7)
        self.assertEqual(review['source_commit'], REVISION7_SOURCE_COMMIT)
        self.assertEqual(review['revision6_source_commit'], REVISION6_SOURCE_COMMIT)
        self.assertEqual(review['revision6_source_tree'], REVISION6_SOURCE_TREE)
        self.assertEqual(review['source_tree'], REVISION7_SOURCE_TREE)
        self.assertEqual(REVISION7_SOURCE_TREE, git('rev-parse', REVISION7_SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision6_snapshots'],
                         {name: dict(commit=REVISION6_COMMIT, **pin) for name, pin in REVISION6_SNAPSHOTS.items()})
        for name, historical in [('transaction', self.rev6_tx), ('compatibility', self.rev6_compat)]:
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 6)
            self.assertEqual(revision['source_commit'], REVISION6_SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION5_COMMIT)
            for field, pin_field in [('previous_snapshot', 'path'), ('previous_snapshot_blob', 'blob'), ('previous_sha256', 'sha256')]:
                self.assertEqual(revision[field], REVISION5_SNAPSHOTS[name][pin_field])

        old = {entry['id']: entry for entry in self.rev6_tx['entries']}
        current = {entry['id']: entry for entry in self.rev7_tx['entries']}
        self.assertEqual(set(old), set(current))
        changes = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(changes), len(review['transaction_callers']))
        self.assertEqual(sorted(changes), REVISION7_EXPECTED['caller_ids'])
        additions, removals, locations = [], [], []
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in changes:
                self.assertEqual(before['callers'], after['callers'])
                continue
            delta = changes[name]
            for prefix, value in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(delta[prefix + '_count'], len(value))
                self.assertEqual(delta[prefix + '_sha256'], hashlib.sha256(gate.canonical_bytes(value)).hexdigest())
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
                removals.append((name, row['caller_id'], row['expression']))
            for row in delta['locations']:
                matches = [c for c in transformed if c['caller_id'] == row['caller_id'] and c['file'] == row['source']
                           and c['expression'] == row['expression'] and c['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                self.assertNotEqual(row['before_line'], row['after_line'])
                matches[0]['line'] = row['after_line']
                locations.append(dict(id=name, **row))
            for row in delta['added']:
                transformed.append(row)
                additions.append((name, row['caller_id'], row['expression']))
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        self.assertEqual(locations, REVISION7_EXPECTED['caller_locations'])
        self.assertEqual(additions, REVISION7_EXPECTED['callers_added'])
        self.assertEqual(removals, REVISION7_EXPECTED['callers_removed'])
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev6_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev7_tx.items() if k not in mutable})
        self.assertEqual(self.rev7_tx['baseline_commit'], REVISION7_SOURCE_COMMIT)
        self.assertEqual(self.rev7_tx['last_review_base_commit'], REVISION7_SOURCE_COMMIT)
        self.assertFalse(review['transaction']['task_complete'])
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['compatibility']['task_complete'])
        self.assertFalse(review['compatibility']['product_green'])


    def test_revision7_client_facts_source_bytes_and_historical_guards_are_bound(self):
        review = json.loads(git('show', REVISION7_COMMIT + ':' + str((REVISION7_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        documented = self.revision7['client_routes']
        self.assertEqual(len(self.revision6['clients']), 133)
        self.assertEqual(len(self.revision7['clients']), REVISION7_EXPECTED['client_branch_count'])
        self.assertEqual(len(documented), REVISION7_EXPECTED['client_branch_count'])
        self.assertEqual(sum(row['forwarding'] for row in self.revision7['clients']), 7)
        indices = REVISION7_EXPECTED['client_previous_indices']
        self.assertEqual(review['client_previous_indices'], indices)
        self.assertEqual(sorted(i for i in indices if i is not None), list(range(133)))
        self.assertEqual([i for i, previous in enumerate(indices) if previous is None], [17])
        self.assertEqual(review['clients_added'], REVISION7_EXPECTED['clients_added'])
        self.assertEqual(review['clients_removed'], [])
        additions = {row['after_index']: row for row in review['clients_added']}
        self.assertEqual(len(additions), 1)
        changes, reordered = [], []
        for index, previous in enumerate(indices):
            after = self.revision7['clients'][index]
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            if previous is None:
                addition = additions[index]
                self.assertEqual(after, addition['row'])
                reference = addition['route_reference_index']
                before = self.revision6['clients'][reference]
                stable = lambda row: {k: v for k, v in row.items() if k not in {'line', 'source', 'owner', 'expression'}}
                self.assertEqual(stable(before), stable(after))
                self.assertEqual(documented[index]['routes'], self.revision6['client_routes'][reference]['routes'])
                continue
            before = self.revision6['clients'][previous]
            expected = {k: v for k, v in before.items() if k != 'line'}
            expected.update(REVISION7_EXPECTED['client_overrides'].get(index, {}))
            self.assertEqual(expected, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual(self.revision6['client_routes'][previous]['routes'], documented[index]['routes'])
            if before != after:
                changes.append(dict(before_index=previous, after_index=index, path=after['path'], methods=after['methods'],
                    changes={k: dict(before=before[k], after=after[k]) for k in before if before[k] != after[k]}))
            if previous != index:
                reordered.append(dict(before_index=previous, after_index=index))
        self.assertEqual([(row['before_index'], row['after_index']) for row in changes], REVISION7_EXPECTED['client_changed_indices'])
        self.assertEqual(review['client_changes'], changes)
        self.assertEqual(review['client_reorder'], reordered)
        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev6_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev7_compat.items() if k not in mutable})
        self.assertEqual(self.rev7_compat['acquisition'], dict(self.rev6_compat['acquisition'], base_commit=REVISION7_SOURCE_COMMIT))
        self.assertEqual(len(self.rev6_compat['routes']), len(self.rev7_compat['routes']))
        changed_routes = []
        for before, after in zip(self.rev6_compat['routes'], self.rev7_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                changed_routes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual(sorted(row['route'] for row in changed_routes), REVISION7_EXPECTED['changed_routes'])
        self.assertEqual(review['compatibility_routes'], changed_routes)
        self.assertEqual(len(self.rev7_compat['source_closure']), 94)
        self.assertEqual(set(self.rev6_compat['source_closure']), set(self.rev7_compat['source_closure']))
        changed = sorted(path for path, digest in self.rev7_compat['source_closure'].items()
                         if digest != self.rev6_compat['source_closure'][path])
        self.assertEqual(changed, REVISION7_EXPECTED['closure_paths'])
        self.assertEqual(review['closure_changes'], changed)
        self.assertEqual([row['path'] for row in review['source_changes']], REVISION7_EXPECTED['source_paths'])
        for row in review['source_changes']:
            self.assertEqual(row['before_commit'], REVISION6_SOURCE_COMMIT)
            self.assertEqual(row['after_commit'], REVISION7_SOURCE_COMMIT)
            path = row['path']
            if git('ls-tree', REVISION6_SOURCE_COMMIT, '--', path):
                before = git('show', REVISION6_SOURCE_COMMIT + ':' + path)
                self.assertEqual(git('rev-parse', REVISION6_SOURCE_COMMIT + ':' + path).decode().strip(), row['before_blob'])
                self.assertEqual(hashlib.sha256(before).hexdigest(), row['before_sha256'])
            else:
                before = None
                self.assertIsNone(row['before_blob'])
                self.assertIsNone(row['before_sha256'])
            raw = git('show', REVISION7_SOURCE_COMMIT + ':' + path)
            self.assertNotEqual(before, raw)
            self.assertEqual(git('rev-parse', REVISION7_SOURCE_COMMIT + ':' + path).decode().strip(), row['after_blob'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), row['after_sha256'])
            self.assertEqual(git('show', REVISION7_SOURCE_COMMIT + ':' + path), raw)
        self.assertEqual(set(review['generated_catalogs']), set(REVISION6_SNAPSHOTS))
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION6_SNAPSHOTS[name]['path'])
            self.assertEqual(hashlib.sha256(git('show', REVISION7_COMMIT + ':' + value['path'])).hexdigest(), value['sha256'])
        previous_review = json.loads(git('show', REVISION6_COMMIT + ':' + str((REVISION6_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['unchanged_validators_and_collectors'], previous_review['unchanged_validators_and_collectors'])
        self.assertEqual(len(review['unchanged_validators_and_collectors']), 5)
        for path, digest in review['unchanged_validators_and_collectors'].items():
            raw = git('show', REVISION6_COMMIT + ':' + path)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
            self.assertEqual(git('show', REVISION7_SOURCE_COMMIT + ':' + path), raw)
        historical_files = 0
        for directory in (EVIDENCE, REVISION3_EVIDENCE, REVISION4_EVIDENCE, REVISION5_EVIDENCE, REVISION6_EVIDENCE):
            names = git('ls-tree', '-r', '--name-only', REVISION6_COMMIT, '--', str(directory.relative_to(ROOT))).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(p.relative_to(ROOT)) for p in directory.rglob('*') if p.is_file()}, set(names))
            historical_files += len(names)
            for path in names:
                self.assertEqual((ROOT / path).read_bytes(), git('show', REVISION6_COMMIT + ':' + path))
        self.assertEqual(historical_files, 35)
        test_path = 'scripts/tests/test_r5_catalog_reconciliation.py'
        def methods(raw):
            return {node.name: ast.dump(node, include_attributes=False) for node in ast.walk(ast.parse(raw))
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        before, after = methods(git('show', REVISION6_COMMIT + ':' + test_path)), methods(git('show', REVISION7_COMMIT + ':' + test_path))
        self.assertEqual(len(before), 15)
        self.assertEqual(len(after), 17)
        self.assertTrue(set(before).issubset(after))
        mutations = {name for name in before if name.startswith('test_unapproved_')}
        self.assertEqual(len(mutations), 5)
        for name in mutations:
            self.assertEqual(before[name], after[name])


    def test_revision8_reviews_the_new_revocation_sql_and_complete_transaction_deltas(self):
        review = revision8_review()
        self.assertEqual(review['inventory_revision'], 8)
        self.assertEqual(review['source_commit'], REVISION8_SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], REVISION8_SOURCE_TREE)
        self.assertEqual(REVISION8_SOURCE_TREE, git('rev-parse', REVISION8_SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision7_source_commit'], REVISION7_SOURCE_COMMIT)
        self.assertEqual(review['revision7_source_tree'], REVISION7_SOURCE_TREE)
        self.assertEqual(review['revision7_snapshots'],
                         {name: dict(commit=REVISION7_COMMIT, **pin) for name, pin in REVISION7_SNAPSHOTS.items()})
        for field in ('transaction_callers', 'transaction_syntax', 'transaction_review_fields'):
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(),
                             REVISION8_EXPECTED[field + '_sha256'])
        old = {e['id']: e for e in self.rev7_tx['entries']}
        current = {e['id']: e for e in self.rev8_tx['entries']}
        self.assertEqual(set(old), set(current))
        self.assertEqual(len(current), 395)
        syntax = {r['id']: r for r in review['transaction_syntax']}
        self.assertEqual(len(syntax), 8)
        grant = 'internal/store/postgres/company_templates.go:*PgStore:SetTemplateGrant'
        self.assertEqual([name for name in old if old[name]['syntax']['sha256'] != current[name]['syntax']['sha256']], [grant])
        callers = {r['id']: r for r in review['transaction_callers']}
        self.assertEqual(len(callers), len(review['transaction_callers']))
        self.assertEqual(len(callers), 61)
        self.assertEqual(sum(len(r['locations']) for r in callers.values()), 79)
        self.assertEqual(sum(len(r['added']) for r in callers.values()), 9)
        self.assertEqual(sum(len(r['removed']) for r in callers.values()), 6)
        manual = {r['id']: r['changes'] for r in review['transaction_review_fields']}
        self.assertEqual(set(manual), set(syntax))
        for name, before in old.items():
            after = current[name]
            expected = copy.deepcopy(before)
            if name in syntax:
                delta = syntax[name]
                self.assertEqual(delta['before'], before['syntax'])
                self.assertEqual(delta['after'], after['syntax'])
                if name != grant:
                    shifted = copy.deepcopy(before['syntax'])
                    shifted['line'] += 12
                    shifted['end'] += 12
                    for field in ('calls', 'strings'):
                        for row in shifted[field]:
                            row['line'] += 12
                    self.assertEqual(shifted, after['syntax'])
                expected['syntax'] = copy.deepcopy(after['syntax'])
                f = after['syntax']
                expected['assertions'].update(
                    direct_sql_effects=[{'line': r['line'], 'sql_fragment': r['value']} for r in f['strings'] if tx.MUTATION.search(r['value'])],
                    direct_lock_fragments=[{'line': r['line'], 'sql_fragment': r['value']} for r in f['strings'] if tx.LOCK.search(r['value'])],
                    transaction_helper_calls=[r for r in f['calls'] if r['name'] in tx.TX],
                    sql_execution_expressions=[r for r in f['calls'] if r['name'] in tx.SQL_CALLS])
                changes = manual[name]
                self.assertEqual(set(changes), {'evidence', 'lock_fk_wait_fence', 'assertions.reviewed_revocation_subject'} if name == grant else {'evidence'})
                for field, change in changes.items():
                    if field == 'assertions.reviewed_revocation_subject':
                        self.assertNotIn('reviewed_revocation_subject', before['assertions'])
                        self.assertIsNone(change['before'])
                        self.assertEqual(change['after'], REVISION8_REVIEWED_SUBJECT)
                        expected['assertions']['reviewed_revocation_subject'] = change['after']
                    else:
                        self.assertEqual(before[field], change['before'])
                        expected[field] = change['after']
                oldref = f"{before['syntax']['file']}:{before['syntax']['line']}-{before['syntax']['end']} sha256={before['syntax']['sha256']}"
                newref = f"{f['file']}:{f['line']}-{f['end']} sha256={f['sha256']}"
                evidence = [newref if item == oldref else item for item in before['evidence']]
                if name == grant:
                    evidence.append('docs/company-mail/evidence/R5-CATALOG-REVISION8-20261008/README.md: static SetTemplateGrant revocation review; no runtime recertification')
                self.assertEqual(after['evidence'], evidence)
            if name in callers:
                delta = callers[name]
                for prefix, value in [('before', before['callers']), ('after', after['callers'])]:
                    self.assertEqual(delta[prefix + '_count'], len(value))
                    self.assertEqual(delta[prefix + '_sha256'], hashlib.sha256(gate.canonical_bytes(value)).hexdigest())
                transformed = copy.deepcopy(before['callers'])
                for row in delta['removed']:
                    transformed.remove(row)
                for row in delta['locations']:
                    matches = [c for c in transformed if c['caller_id'] == row['caller_id'] and c['file'] == row['source']
                               and c['expression'] == row['expression'] and c['line'] == row['before_line']]
                    self.assertEqual(len(matches), 1)
                    self.assertNotEqual(row['before_line'], row['after_line'])
                    matches[0]['line'] = row['after_line']
                transformed.extend(delta['added'])
                self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                                 Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
                expected['callers'] = copy.deepcopy(after['callers'])
            self.assertEqual(expected, after)
        oldfiles = {r['path']: r['sha256'] for r in self.rev7_tx['postgres_files']}
        files = {r['path']: r['sha256'] for r in self.rev8_tx['postgres_files']}
        self.assertEqual(set(oldfiles), set(files))
        self.assertEqual([p for p in files if files[p] != oldfiles[p]], ['internal/store/postgres/company_templates.go'])
        mutable = {'entries', 'postgres_files', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev7_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev8_tx.items() if k not in mutable})
        self.assertEqual(self.rev8_tx['baseline_commit'], REVISION8_SOURCE_COMMIT)
        self.assertEqual(self.rev8_tx['last_review_base_commit'], REVISION8_SOURCE_COMMIT)
        self.assertEqual(review['sql_review']['manual_review'], REVISION8_REVIEWED_SUBJECT)
        self.assertEqual(review['sql_review']['new_sql'], 'SELECT EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND id=$2)')
        self.assertEqual(review['sql_review']['before_sql_execution_calls'], 498)
        self.assertEqual(review['sql_review']['after_sql_execution_calls'], 499)
        self.assertEqual(len(old[grant]['assertions']['sql_execution_expressions']), 3)
        self.assertEqual(len(current[grant]['assertions']['sql_execution_expressions']), 4)
        # This is the immutable published review result, not a new validator
        # execution on reconstructed catalog facts. Current source is collected
        # independently and validated by the current-revision tests.
        self.assertEqual(review['transaction'], {'status': 'PASS', 'postgres_files': 62, 'functions': 395, 'sql_execution_calls': 499, 'direct_write_functions': 134, 'write_closure_functions': 154, 'migration_files': 19, 'task_complete': False, 'runtime_verified': False, 'meaning': 'syntax inventory current; no concurrency or behavior equivalence claim'})
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['transaction']['task_complete'])

    def test_revision8_preserves_client_closure_history_and_all_original_guards(self):
        review = revision8_review()
        self.assertEqual(len(self.revision8['clients']), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.revision8['clients']), 7)
        self.assertEqual(review['client_previous_indices'], list(range(134)))
        for field in ('clients_added', 'clients_removed', 'client_reorder'):
            self.assertEqual(review[field], [])
        documented = self.revision8['client_routes']
        changes = []
        for index, (before, after) in enumerate(zip(self.revision7['clients'], self.revision8['clients'])):
            self.assertEqual({k: v for k, v in before.items() if k != 'line'}, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            self.assertEqual(documented[index]['routes'], self.revision7['client_routes'][index]['routes'])
            if before != after:
                changes.append(dict(before_index=index, after_index=index, path=after['path'], methods=after['methods'],
                    changes={k: dict(before=before[k], after=after[k]) for k in before if before[k] != after[k]}))
        self.assertEqual(len(documented), 134)
        self.assertEqual(changes, REVISION8_EXPECTED['client_changes'])
        self.assertEqual(review['client_changes'], changes)
        self.assertEqual(self.revision8['clients'][17], self.revision7['clients'][17])
        self.assertEqual(len(self.rev8_compat['routes']), 132)
        route_changes = []
        for before, after in zip(self.rev7_compat['routes'], self.rev8_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                route_changes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual(route_changes, [{'route': 'POST /api/v1/company/mailboxes', 'changed_fields': ['clients']}])
        self.assertEqual(review['compatibility_routes'], route_changes)
        self.assertEqual(set(self.rev8_compat['source_closure']), set(self.rev7_compat['source_closure']))
        self.assertEqual(len(self.rev8_compat['source_closure']), 94)
        closure_changes = sorted(path for path, digest in self.rev8_compat['source_closure'].items()
                                 if digest != self.rev7_compat['source_closure'][path])
        self.assertEqual(closure_changes, REVISION8_EXPECTED['closure_paths'])
        self.assertEqual(review['closure_changes'], closure_changes)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.rev8_compat['source_closure'])).hexdigest(), REVISION8_EXPECTED['source_closure_sha256'])
        mutable = {'inventory_revision', 'acquisition', 'routes', 'source_closure'}
        self.assertEqual({k: v for k, v in self.rev7_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev8_compat.items() if k not in mutable})
        self.assertEqual(self.rev8_compat['acquisition'], dict(self.rev7_compat['acquisition'], base_commit=REVISION8_SOURCE_COMMIT))
        # Review identity and complete deltas bind historical results; no
        # current product is substituted for the revision-8 source here.
        self.assertEqual(review['compatibility'], {'wire_validation_scope': 'not_checked_current_wire_required', 'historical_wire_reference': {'artifact_ref': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-map-v1.json', 'sha256': '61b039486bc7804366012298fe87203882b6fb52ba1b160eb8ee77d76989dc22', 'qualification': 'historical_metadata_only_not_current_wire'}, 'status': 'source_inventory_and_upgrade_plan_checked', 'task_complete': False, 'product_green': False, 'routes': 132, 'client_branches': 134, 'source_files': 94, 'openapi_missing': ['DELETE /api/v1/suppression/{id}', 'GET /api/v1/suppression', 'GET /docs-assets/*'], 'no_shipped_client': ['DELETE /api/v1/suppression/{id}', 'GET /api/v1/admin/status', 'GET /api/v1/auth/me', 'GET /api/v1/company/outbound/{id}/recipients', 'GET /api/v1/suppression', 'GET /docs', 'GET /docs-assets/*', 'GET /metrics', 'GET /openapi.yaml', 'GET /ready', 'GET /redoc'], 'runtime_boundary': 'No HTTP/DB/old-client upgrade execution; fresh scoped evidence and dependency review remain required.'})
        self.assertFalse(review['compatibility']['product_green'])
        self.assertFalse(review['compatibility']['task_complete'])
        checkpoint = review['checkpoint53']
        self.assertEqual(checkpoint['local_checkpoint_commit'], 'f18d5305be34c1103f74fb335ffd37ebda5fe071')
        self.assertEqual(checkpoint['product_source_commit'], '53cd5de5afddfc661ade306c16ca11da573d49de')
        self.assertEqual(checkpoint['packet_path'], 'docs/company-mail/evidence/R5-CATALOG-REVISION8-20261008/checkpoint53-reconciliation.json')
        self.assertEqual(checkpoint['packet_sha256'], 'af22d61b076965297dff410a91a1855613cb4d33619f6f34784cfc29671e54f8')
        raw = (ROOT / checkpoint['packet_path']).read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), checkpoint['packet_sha256'])
        historical = json.loads(raw)
        for field in ('validation', 'baseline_validation', 'complete_source_runner'):
            self.assertEqual(checkpoint[field], historical[field])
        for name in ('clients', 'client_routes'):
            raw = git('show', REVISION7_COMMIT + ':' + REVISION7_SNAPSHOTS[name]['path'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), historical['generated_catalogs'][name]['sha256'])
        self.assertEqual(historical['client_changes'], [])
        self.assertEqual(historical['closure_changes'], [])
        self.assertEqual(historical['compatibility_routes'], [])
        # Reconstruct only the documented metadata update over public revision-7
        # bytes and match the exact complete catalog digest observed at checkpoint.
        # No local checkpoint Git object is needed by CI or future checkouts.
        frozen_compat = copy.deepcopy(self.rev7_compat)
        frozen_compat['inventory_revision'] = dict(self.rev8_compat['inventory_revision'], source_commit=checkpoint['product_source_commit'])
        frozen_compat['acquisition'] = dict(self.rev7_compat['acquisition'], base_commit=checkpoint['product_source_commit'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(frozen_compat)).hexdigest(), historical['generated_catalogs']['compatibility']['sha256'])
        self.assertEqual(frozen_compat['source_closure'], self.rev7_compat['source_closure'])
        self.assertEqual(frozen_compat['routes'], self.rev7_compat['routes'])
        self.assertEqual(review['transaction_syntax'], historical['transaction_syntax'])
        self.assertEqual(review['transaction_review_fields'], historical['transaction_review_fields'])
        self.assertEqual([r['path'] for r in review['source_changes']], REVISION8_EXPECTED['source_paths'])
        for row in review['source_changes']:
            self.assertEqual(row['before_commit'], REVISION7_SOURCE_COMMIT)
            self.assertEqual(row['after_commit'], REVISION8_SOURCE_COMMIT)
            for prefix, commit in [('before', REVISION7_SOURCE_COMMIT), ('after', REVISION8_SOURCE_COMMIT)]:
                ref = commit + ':' + row['path']
                if prefix == 'before' and row['before_blob'] is None:
                    self.assertFalse(git('ls-tree', commit, '--', row['path']))
                    self.assertIsNone(row['before_sha256'])
                else:
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[prefix + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
            self.assertEqual(git('show', REVISION8_COMMIT + ':' + row['path']), git('show', REVISION8_SOURCE_COMMIT + ':' + row['path']))
        self.assertEqual(review['excluded_closure_product_paths'],
                         ['internal/ratelimit/sliding.go', 'web/app/(dashboard)/account/page.tsx',
                          'web/components/company/send-policy.tsx', 'web/features/mail/components/received-folder.tsx'])
        self.assertTrue(set(review['excluded_closure_product_paths']).isdisjoint(self.rev8_compat['source_closure']))
        self.assertEqual(set(review['generated_catalogs']), set(REVISION7_SNAPSHOTS))
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION7_SNAPSHOTS[name]['path'])
            self.assertEqual(hashlib.sha256(git('show', REVISION8_COMMIT + ':' + value['path'])).hexdigest(), value['sha256'])
        previous = json.loads(git('show', REVISION7_COMMIT + ':' + str((REVISION7_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['unchanged_validators_and_collectors'], previous['unchanged_validators_and_collectors'])
        self.assertEqual(len(review['unchanged_validators_and_collectors']), 5)
        for path, digest in review['unchanged_validators_and_collectors'].items():
            raw = git('show', REVISION7_COMMIT + ':' + path)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
            self.assertEqual((ROOT / path).read_bytes(), raw)
        # Historical runner and compatibility-test bytes stay bound to their published revision.
        for path, digest in review['unchanged_source_runner_files'].items():
            raw = git('show', REVISION8_SOURCE_COMMIT + ':' + path)
            self.assertEqual(git('show', REVISION8_COMMIT + ':' + path), raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
        test_path = review['unchanged_compatibility_test']['path']
        self.assertEqual(test_path, 'scripts/tests/test_r5_compatibility.py')
        self.assertEqual(git('show', REVISION8_COMMIT + ':' + test_path), git('show', REVISION7_COMMIT + ':' + test_path))
        self.assertEqual(hashlib.sha256(git('show', REVISION8_COMMIT + ':' + test_path)).hexdigest(), review['unchanged_compatibility_test']['sha256'])
        actual_paths = []
        for directory in (EVIDENCE, REVISION3_EVIDENCE, REVISION4_EVIDENCE, REVISION5_EVIDENCE, REVISION6_EVIDENCE, REVISION7_EVIDENCE):
            names = git('ls-tree', '-r', '--name-only', REVISION7_COMMIT, '--', str(directory.relative_to(ROOT))).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(p.relative_to(ROOT)) for p in directory.rglob('*') if p.is_file()}, set(names))
            actual_paths.extend(names)
        self.assertEqual(review['historical_files'], 40)
        self.assertEqual([row['path'] for row in review['historical_snapshots']], actual_paths)
        self.assertEqual(len(actual_paths), 40)
        for row in review['historical_snapshots']:
            ref = REVISION7_COMMIT + ':' + row['path']
            raw = git('show', ref)
            self.assertEqual((ROOT / row['path']).read_bytes(), raw)
            self.assertEqual(git('rev-parse', ref).decode().strip(), row['blob'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), row['sha256'])
            self.assertEqual(len(raw), row['bytes'])
        test_path = 'scripts/tests/test_r5_catalog_reconciliation.py'
        def methods(raw):
            return {node.name: node for node in ast.walk(ast.parse(raw))
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        before_raw = git('show', REVISION7_COMMIT + ':' + test_path).decode()
        after_raw = git('show', REVISION8_COMMIT + ':' + test_path).decode()
        before, after = methods(before_raw), methods(after_raw)
        self.assertEqual(len(before), 17)
        self.assertEqual(len(after), 19)
        self.assertTrue(set(before).issubset(after))
        mutations = {name for name in before if name.startswith('test_unapproved_')}
        self.assertEqual(len(mutations), 5)
        for name in mutations:
            self.assertEqual(ast.dump(before[name], include_attributes=False), ast.dump(after[name], include_attributes=False))
            self.assertEqual(ast.get_source_segment(before_raw, before[name]), ast.get_source_segment(after_raw, after[name]))
        self.assertEqual(review['implementation_todos_completed'], 0)
        self.assertFalse(review['runtime_tests_executed'])
        self.assertEqual(review['parent_tasks'], {'accepted': 10, 'total': 171, 'remaining': 161})


    def test_revision9_preserves_manual_reviews_and_binds_fresh_source_facts(self):
        review = revision9_review()
        # Immutable review identity and complete deltas establish historical
        # facts here; these are not fresh executions against a fabricated AST.
        # Current collectors/validators execute in the revision-10 tests.
        self.assertEqual(review['inventory_revision'], 9)
        self.assertEqual(review['source_commit'], REVISION9_SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], REVISION9_SOURCE_TREE)
        self.assertEqual(git('rev-parse', REVISION9_SOURCE_COMMIT + '^{tree}').decode().strip(), REVISION9_SOURCE_TREE)
        self.assertEqual(review['revision8_snapshots'],
                         {name: dict(commit=REVISION8_COMMIT, **pin) for name, pin in REVISION8_SNAPSHOTS.items()})
        for field in ('transaction_callers', 'client_changes', 'compatibility_routes', 'closure_changes', 'source_changes'):
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(),
                             REVISION9_EXPECTED[field + '_sha256'])
        old = {entry['id']: entry for entry in self.rev8_tx['entries']}
        current = {entry['id']: entry for entry in self.rev9_tx['entries']}
        self.assertEqual(set(old), set(current))
        deltas = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(deltas), len(review['transaction_callers']))
        self.assertEqual(set(deltas), {name for name in old if old[name]['callers'] != current[name]['callers']})
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in deltas:
                continue
            delta = deltas[name]
            for prefix, callers in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(len(callers), delta[prefix + '_count'])
                self.assertEqual(hashlib.sha256(gate.canonical_bytes(callers)).hexdigest(), delta[prefix + '_sha256'])
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
            for row in delta['locations']:
                matches = [caller for caller in transformed if caller['caller_id'] == row['caller_id'] and
                           caller['file'] == row['source'] and caller['expression'] == row['expression'] and
                           caller['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                self.assertNotEqual(row['before_line'], row['after_line'])
                matches[0]['line'] = row['after_line']
            transformed.extend(delta['added'])
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev8_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev9_tx.items() if k not in mutable})
        self.assertEqual(self.rev9_tx['baseline_commit'], REVISION9_SOURCE_COMMIT)
        self.assertEqual(self.rev9_tx['last_review_base_commit'], REVISION9_SOURCE_COMMIT)
        self.assertEqual({'status': 'PASS', 'postgres_files': 62, 'functions': 395, 'sql_execution_calls': 499, 'direct_write_functions': 134, 'write_closure_functions': 154, 'migration_files': 19, 'task_complete': False, 'runtime_verified': False, 'meaning': 'syntax inventory current; no concurrency or behavior equivalence claim'}, review['transaction'])
        self.assertEqual({k: review['transaction'][k] for k in
                          ('postgres_files', 'functions', 'migration_files', 'direct_write_functions',
                           'write_closure_functions', 'sql_execution_calls')},
                         dict(postgres_files=62, functions=395, migration_files=19,
                              direct_write_functions=134, write_closure_functions=154, sql_execution_calls=499))
        self.assertEqual(review['transaction_syntax_changes'], [])
        self.assertEqual(review['transaction_manual_review_changes'], [])
        documented = self.revision9['client_routes']
        self.assertEqual(len(self.revision9['clients']), len(self.revision8['clients']))
        self.assertEqual(len(documented), len(self.revision9['clients']))
        client_changes = []
        for index, (before, after) in enumerate(zip(self.revision8['clients'], self.revision9['clients'])):
            self.assertEqual({k: v for k, v in before.items() if k != 'line'},
                             {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            self.assertEqual(documented[index]['routes'], self.revision8['client_routes'][index]['routes'])
            if before != after:
                client_changes.append(dict(index=index, source=after['source'], path=after['path'], methods=after['methods'],
                                           before_line=before['line'], after_line=after['line']))
        self.assertEqual(client_changes, review['client_changes'])
        route_changes = []
        self.assertEqual(len(self.rev8_compat['routes']), len(self.rev9_compat['routes']))
        for before, after in zip(self.rev8_compat['routes'], self.rev9_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'},
                             {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                route_changes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual(route_changes, review['compatibility_routes'])
        self.assertEqual(set(self.rev9_compat['source_closure']), set(self.rev8_compat['source_closure']))
        self.assertEqual(len(self.rev9_compat['source_closure']), 94)
        closure_changes = [dict(path=path, before_sha256=digest, after_sha256=self.rev9_compat['source_closure'][path])
                           for path, digest in self.rev8_compat['source_closure'].items()
                           if digest != self.rev9_compat['source_closure'][path]]
        self.assertEqual(closure_changes, review['closure_changes'])
        mutable = {'inventory_revision', 'acquisition', 'routes', 'source_closure'}
        self.assertEqual({k: v for k, v in self.rev8_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev9_compat.items() if k not in mutable})
        self.assertEqual(self.rev9_compat['acquisition'], dict(self.rev8_compat['acquisition'], base_commit=REVISION9_SOURCE_COMMIT))
        self.assertEqual({'wire_validation_scope': 'not_checked_current_wire_required', 'historical_wire_reference': {'artifact_ref': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-map-v1.json', 'sha256': '61b039486bc7804366012298fe87203882b6fb52ba1b160eb8ee77d76989dc22', 'qualification': 'historical_metadata_only_not_current_wire'}, 'status': 'source_inventory_and_upgrade_plan_checked', 'task_complete': False, 'product_green': False, 'routes': 132, 'client_branches': 134, 'source_files': 94, 'openapi_missing': ['DELETE /api/v1/suppression/{id}', 'GET /api/v1/suppression', 'GET /docs-assets/*'], 'no_shipped_client': ['DELETE /api/v1/suppression/{id}', 'GET /api/v1/admin/status', 'GET /api/v1/auth/me', 'GET /api/v1/company/outbound/{id}/recipients', 'GET /api/v1/suppression', 'GET /docs', 'GET /docs-assets/*', 'GET /metrics', 'GET /openapi.yaml', 'GET /ready', 'GET /redoc'], 'runtime_boundary': 'No HTTP/DB/old-client upgrade execution; fresh scoped evidence and dependency review remain required.'}, review['compatibility'])
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION8_SNAPSHOTS[name]['path'])
            raw = git('show', REVISION9_COMMIT + ':' + value['path'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), value['sha256'])
            self.assertEqual(len(raw), value['bytes'])
        self.assertEqual(set(review['generated_catalogs']), set(REVISION8_SNAPSHOTS))
        self.assertEqual([row['path'] for row in review['source_changes']], REVISION9_EXPECTED['source_paths'])
        changed = git('diff', '--name-only', REVISION8_SOURCE_COMMIT, REVISION9_SOURCE_COMMIT, '--', 'internal', 'cmd', 'web').decode().splitlines()
        product_paths = [p for p in changed if (p.endswith('.go') and not p.endswith('_test.go')) or
                         (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p)]
        self.assertEqual(product_paths, REVISION9_EXPECTED['source_paths'])
        self.assertEqual(review['excluded_closure_product_paths'], [path for path in product_paths if path not in self.rev9_compat['source_closure']])
        for row in review['source_changes']:
            self.assertEqual(row['before_commit'], REVISION8_SOURCE_COMMIT)
            self.assertEqual(row['after_commit'], REVISION9_SOURCE_COMMIT)
            for prefix, commit in [('before', REVISION8_SOURCE_COMMIT), ('after', REVISION9_SOURCE_COMMIT)]:
                ref = commit + ':' + row['path']
                if prefix == 'before' and row['before_blob'] is None:
                    self.assertFalse(git('ls-tree', commit, '--', row['path']))
                    self.assertIsNone(row['before_sha256'])
                else:
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[prefix + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
            self.assertEqual(git('show', REVISION9_COMMIT + ':' + row['path']), git('show', REVISION9_SOURCE_COMMIT + ':' + row['path']))
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['transaction']['task_complete'])
        self.assertFalse(review['compatibility']['product_green'])
        self.assertFalse(review['compatibility']['task_complete'])
        self.assertFalse(review['runtime_verified'])
        self.assertFalse(review['product_green'])
        self.assertFalse(review['task_complete'])
        self.assertEqual(review['implementation_todos_completed'], 0)
        self.assertEqual(review['parent_tasks'], dict(accepted=10, total=171, remaining=161))

    def test_revision9_preserves_all_historical_bytes_and_original_rejection_guards(self):
        review = revision9_review()
        directories = (EVIDENCE, REVISION3_EVIDENCE, REVISION4_EVIDENCE, REVISION5_EVIDENCE,
                       REVISION6_EVIDENCE, REVISION7_EVIDENCE, REVISION8_EVIDENCE)
        manifest = []
        for directory in directories:
            names = git('ls-tree', '-r', '--name-only', REVISION8_COMMIT, '--', str(directory.relative_to(ROOT))).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(p.relative_to(ROOT)) for p in directory.rglob('*') if p.is_file()}, set(names))
            for path in names:
                ref = REVISION8_COMMIT + ':' + path
                raw = git('show', ref)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                manifest.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                     sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
        self.assertEqual(len(manifest), 43)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(manifest)).hexdigest(), REVISION9_EXPECTED['historical_manifest_sha256'])
        self.assertEqual(review['historical_files'], 43)
        self.assertEqual(review['historical_manifest_sha256'], REVISION9_EXPECTED['historical_manifest_sha256'])
        test_path = 'scripts/tests/test_r5_catalog_reconciliation.py'
        protected_paths = git('ls-tree', '-r', '--name-only', REVISION8_COMMIT, '--', 'scripts', '.github/workflows',
                              'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go').decode().splitlines()
        protected = []
        for path in protected_paths:
            if path == test_path:
                continue
            ref = REVISION8_COMMIT + ':' + path
            raw = git('show', ref)
            self.assertEqual(git('show', REVISION9_COMMIT + ':' + path), raw)
            protected.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(), sha256=hashlib.sha256(raw).hexdigest()))
        self.assertEqual(len(protected), 164)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(protected)).hexdigest(), REVISION9_EXPECTED['protected_source_manifest_sha256'])
        self.assertEqual(review['protected_sources'], 164)
        self.assertEqual(review['protected_source_manifest_sha256'], REVISION9_EXPECTED['protected_source_manifest_sha256'])
        before_raw = git('show', REVISION8_COMMIT + ':' + test_path).decode()
        after_raw = git('show', REVISION9_COMMIT + ':' + test_path).decode()
        def methods(raw):
            return {node.name: node for node in ast.walk(ast.parse(raw))
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        before, after = methods(before_raw), methods(after_raw)
        self.assertEqual(len(before), 19)
        self.assertEqual(len(after), 21)
        self.assertTrue(set(before).issubset(after))
        mutations = {name for name in before if name.startswith('test_unapproved_')}
        self.assertEqual(len(mutations), 5)
        for name in mutations:
            self.assertEqual(ast.dump(before[name], include_attributes=False), ast.dump(after[name], include_attributes=False))
            self.assertEqual(ast.get_source_segment(before_raw, before[name]), ast.get_source_segment(after_raw, after[name]))
        # Preserve the exact published rejection contracts and their original
        # test source, without substituting a new product for revision 9.
        fixed_tree = ast.parse(after_raw)
        fixed_expected = next(ast.literal_eval(node.value) for node in fixed_tree.body
                              if isinstance(node, ast.Assign) and any(
                                  isinstance(t, ast.Name) and t.id == 'REVISION9_EXPECTED' for t in node.targets))
        self.assertEqual(fixed_expected, REVISION9_EXPECTED)
        self.assertEqual(review['transaction_revision8_rejection'], REVISION9_EXPECTED['transaction_revision8_rejection'])
        self.assertEqual(review['compatibility_rejections'], REVISION9_EXPECTED['compatibility_rejections'])

    def test_revision10_preserves_manual_reviews_and_binds_fresh_source_facts(self):
        review = revision10_review()
        # Immutable review/catalog identities preserve revision-10 acceptance;
        # current original collectors and validators execute in revision 11.
        self.assertEqual(review['inventory_revision'], 10)
        self.assertEqual(review['source_commit'], REVISION10_SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], REVISION10_SOURCE_TREE)
        self.assertEqual(git('rev-parse', REVISION10_SOURCE_COMMIT + '^{tree}').decode().strip(), REVISION10_SOURCE_TREE)
        self.assertEqual(review['revision9_snapshots'],
                         {name: dict(commit=REVISION9_COMMIT, **pin) for name, pin in REVISION9_SNAPSHOTS.items()})
        for field in ('transaction_callers', 'client_changes', 'compatibility_routes', 'closure_changes', 'source_changes'):
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(),
                             REVISION10_EXPECTED[field + '_sha256'])
        old = {entry['id']: entry for entry in self.rev9_tx['entries']}
        current = {entry['id']: entry for entry in self.rev10_tx['entries']}
        self.assertEqual(set(old), set(current))
        deltas = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(deltas), len(review['transaction_callers']))
        self.assertEqual(set(deltas), {name for name in old if old[name]['callers'] != current[name]['callers']})
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in deltas:
                continue
            delta = deltas[name]
            for prefix, callers in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(len(callers), delta[prefix + '_count'])
                self.assertEqual(hashlib.sha256(gate.canonical_bytes(callers)).hexdigest(), delta[prefix + '_sha256'])
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
            for row in delta['locations']:
                matches = [caller for caller in transformed if caller['caller_id'] == row['caller_id'] and
                           caller['file'] == row['source'] and caller['expression'] == row['expression'] and
                           caller['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                self.assertNotEqual(row['before_line'], row['after_line'])
                matches[0]['line'] = row['after_line']
            transformed.extend(delta['added'])
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev9_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev10_tx.items() if k not in mutable})
        self.assertEqual(self.rev10_tx['baseline_commit'], REVISION10_SOURCE_COMMIT)
        self.assertEqual(self.rev10_tx['last_review_base_commit'], REVISION10_SOURCE_COMMIT)
        self.assertEqual({'status': 'PASS', 'postgres_files': 62, 'functions': 395, 'sql_execution_calls': 499, 'direct_write_functions': 134, 'write_closure_functions': 154, 'migration_files': 19, 'task_complete': False, 'runtime_verified': False, 'meaning': 'syntax inventory current; no concurrency or behavior equivalence claim'}, review['transaction'])
        self.assertEqual({k: review['transaction'][k] for k in
                          ('postgres_files', 'functions', 'migration_files', 'direct_write_functions',
                           'write_closure_functions', 'sql_execution_calls')},
                         dict(postgres_files=62, functions=395, migration_files=19,
                              direct_write_functions=134, write_closure_functions=154, sql_execution_calls=499))
        self.assertEqual(review['transaction_syntax_changes'], [])
        self.assertEqual(review['transaction_manual_review_changes'], [])
        documented = self.revision10['client_routes']
        self.assertEqual(len(self.revision10['clients']), len(self.revision9['clients']))
        self.assertEqual(len(documented), len(self.revision10['clients']))
        client_changes = []
        for index, (before, after) in enumerate(zip(self.revision9['clients'], self.revision10['clients'])):
            self.assertEqual({k: v for k, v in before.items() if k != 'line'},
                             {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            self.assertEqual(documented[index]['routes'], self.revision9['client_routes'][index]['routes'])
            if before != after:
                client_changes.append(dict(index=index, source=after['source'], path=after['path'], methods=after['methods'],
                                           before_line=before['line'], after_line=after['line']))
        self.assertEqual(client_changes, review['client_changes'])
        route_changes = []
        self.assertEqual(len(self.rev9_compat['routes']), len(self.rev10_compat['routes']))
        for before, after in zip(self.rev9_compat['routes'], self.rev10_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'},
                             {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                route_changes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual(route_changes, review['compatibility_routes'])
        self.assertEqual(set(self.rev10_compat['source_closure']), set(self.rev9_compat['source_closure']))
        self.assertEqual(len(self.rev10_compat['source_closure']), 94)
        closure_changes = [dict(path=path, before_sha256=digest, after_sha256=self.rev10_compat['source_closure'][path])
                           for path, digest in self.rev9_compat['source_closure'].items()
                           if digest != self.rev10_compat['source_closure'][path]]
        self.assertEqual(closure_changes, review['closure_changes'])
        mutable = {'inventory_revision', 'acquisition', 'routes', 'source_closure'}
        self.assertEqual({k: v for k, v in self.rev9_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev10_compat.items() if k not in mutable})
        self.assertEqual(self.rev10_compat['acquisition'], dict(self.rev9_compat['acquisition'], base_commit=REVISION10_SOURCE_COMMIT))
        self.assertEqual({'wire_validation_scope': 'not_checked_current_wire_required', 'historical_wire_reference': {'artifact_ref': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-map-v1.json', 'sha256': '61b039486bc7804366012298fe87203882b6fb52ba1b160eb8ee77d76989dc22', 'qualification': 'historical_metadata_only_not_current_wire'}, 'status': 'source_inventory_and_upgrade_plan_checked', 'task_complete': False, 'product_green': False, 'routes': 132, 'client_branches': 134, 'source_files': 94, 'openapi_missing': ['DELETE /api/v1/suppression/{id}', 'GET /api/v1/suppression', 'GET /docs-assets/*'], 'no_shipped_client': ['DELETE /api/v1/suppression/{id}', 'GET /api/v1/admin/status', 'GET /api/v1/auth/me', 'GET /api/v1/company/outbound/{id}/recipients', 'GET /api/v1/suppression', 'GET /docs', 'GET /docs-assets/*', 'GET /metrics', 'GET /openapi.yaml', 'GET /ready', 'GET /redoc'], 'runtime_boundary': 'No HTTP/DB/old-client upgrade execution; fresh scoped evidence and dependency review remain required.'}, review['compatibility'])
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION9_SNAPSHOTS[name]['path'])
            raw = git('show', REVISION10_COMMIT + ':' + value['path'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), value['sha256'])
            self.assertEqual(len(raw), value['bytes'])
        self.assertEqual(set(review['generated_catalogs']), set(REVISION9_SNAPSHOTS))
        self.assertEqual([row['path'] for row in review['source_changes']], REVISION10_EXPECTED['source_paths'])
        changed = git('diff', '--name-only', REVISION9_SOURCE_COMMIT, REVISION10_SOURCE_COMMIT, '--', 'internal', 'cmd', 'web').decode().splitlines()
        product_paths = [p for p in changed if (p.endswith('.go') and not p.endswith('_test.go')) or
                         (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p) or
                         (p.startswith('web/locales/') and p.endswith('.json'))]
        self.assertEqual(product_paths, REVISION10_EXPECTED['source_paths'])
        self.assertEqual(review['excluded_closure_product_paths'], [path for path in product_paths if path not in self.rev10_compat['source_closure']])
        for row in review['source_changes']:
            self.assertEqual(row['before_commit'], REVISION9_SOURCE_COMMIT)
            self.assertEqual(row['after_commit'], REVISION10_SOURCE_COMMIT)
            for prefix, commit in [('before', REVISION9_SOURCE_COMMIT), ('after', REVISION10_SOURCE_COMMIT)]:
                ref = commit + ':' + row['path']
                if prefix == 'before' and row['before_blob'] is None:
                    self.assertFalse(git('ls-tree', commit, '--', row['path']))
                    self.assertIsNone(row['before_sha256'])
                else:
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[prefix + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
            self.assertEqual(git('show', REVISION10_COMMIT + ':' + row['path']), git('show', REVISION10_SOURCE_COMMIT + ':' + row['path']))
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['transaction']['task_complete'])
        self.assertFalse(review['compatibility']['product_green'])
        self.assertFalse(review['compatibility']['task_complete'])
        self.assertFalse(review['runtime_verified'])
        self.assertFalse(review['product_green'])
        self.assertFalse(review['task_complete'])
        self.assertEqual(review['implementation_todos_completed'], 0)
        self.assertEqual(review['parent_tasks'], dict(accepted=10, total=171, remaining=161))

    def test_revision10_preserves_all_historical_bytes_and_original_rejection_guards(self):
        review = revision10_review()
        directories = (EVIDENCE, REVISION3_EVIDENCE, REVISION4_EVIDENCE, REVISION5_EVIDENCE,
                       REVISION6_EVIDENCE, REVISION7_EVIDENCE, REVISION8_EVIDENCE, REVISION9_EVIDENCE)
        manifest = []
        for directory in directories:
            names = git('ls-tree', '-r', '--name-only', REVISION9_COMMIT, '--', str(directory.relative_to(ROOT))).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(p.relative_to(ROOT)) for p in directory.rglob('*') if p.is_file()}, set(names))
            for path in names:
                ref = REVISION9_COMMIT + ':' + path
                raw = git('show', ref)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                manifest.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                     sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
        self.assertEqual(len(manifest), 45)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(manifest)).hexdigest(), REVISION10_EXPECTED['historical_manifest_sha256'])
        self.assertEqual(review['historical_files'], 45)
        self.assertEqual(review['historical_manifest_sha256'], REVISION10_EXPECTED['historical_manifest_sha256'])
        test_path = 'scripts/tests/test_r5_catalog_reconciliation.py'
        protected_paths = git('ls-tree', '-r', '--name-only', REVISION9_COMMIT, '--', 'scripts', '.github/workflows',
                              'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go').decode().splitlines()
        protected = []
        for path in protected_paths:
            if path == test_path:
                continue
            ref = REVISION9_COMMIT + ':' + path
            raw = git('show', ref)
            self.assertEqual(git('show', REVISION10_COMMIT + ':' + path), raw)
            protected.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(), sha256=hashlib.sha256(raw).hexdigest()))
        self.assertEqual(len(protected), 164)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(protected)).hexdigest(), REVISION10_EXPECTED['protected_source_manifest_sha256'])
        self.assertEqual(review['protected_sources'], 164)
        self.assertEqual(review['protected_source_manifest_sha256'], REVISION10_EXPECTED['protected_source_manifest_sha256'])
        before_raw = git('show', REVISION9_COMMIT + ':' + test_path).decode()
        after_raw = git('show', REVISION10_COMMIT + ':' + test_path).decode()
        def methods(raw):
            return {node.name: node for node in ast.walk(ast.parse(raw))
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        before, after = methods(before_raw), methods(after_raw)
        self.assertEqual(len(before), 21)
        self.assertEqual(len(after), 23)
        self.assertTrue(set(before).issubset(after))
        mutations = {name for name in before if name.startswith('test_unapproved_')}
        self.assertEqual(len(mutations), 5)
        for name in mutations:
            self.assertEqual(ast.dump(before[name], include_attributes=False), ast.dump(after[name], include_attributes=False))
            self.assertEqual(ast.get_source_segment(before_raw, before[name]), ast.get_source_segment(after_raw, after[name]))
        # Exact historical rejection contracts stay attached to the immutable
        # published review; no fabricated old AST or current-source substitution.
        self.assertEqual(review['transaction_revision9_rejection'], REVISION10_EXPECTED['transaction_revision9_rejection'])
        self.assertEqual(review['compatibility_rejections'], REVISION10_EXPECTED['compatibility_rejections'])
        fixed_tree = ast.parse(before_raw)
        fixed_expected = next(ast.literal_eval(node.value) for node in fixed_tree.body
                              if isinstance(node, ast.Assign) and any(
                                  isinstance(t, ast.Name) and t.id == 'REVISION9_EXPECTED' for t in node.targets))
        self.assertEqual(fixed_expected, REVISION9_EXPECTED)


    def test_revision11_reconciles_actual_functions_clients_routes_and_source(self):
        with self.revision11_context():
            review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
            self.assertEqual(review['inventory_revision'], 11)
            self.assertEqual(review['source_commit'], SOURCE_COMMIT)
            self.assertEqual(review['source_tree'], SOURCE_TREE)
            self.assertEqual(git('rev-parse', SOURCE_COMMIT + '^{tree}').decode().strip(), SOURCE_TREE)
            self.assertEqual(review['revision10_snapshots'],
                             {name: dict(commit=REVISION10_COMMIT, **pin) for name, pin in REVISION10_SNAPSHOTS.items()})
            for field, digest in REVISION11_EXPECTED['review_field_sha256'].items():
                self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(), digest)
            self.assertEqual(tx.validate(self.tx, self.ast, self.migrations), review['transaction'])
            self.assertEqual(gate.validate(self.compat, self.routes, self.clients), review['compatibility'])
            self.assertEqual(review['transaction'], REVISION11_EXPECTED['transaction'])
            self.assertEqual({key: review['compatibility'][key] for key in ('routes', 'client_branches', 'source_files')},
                             dict(routes=133, client_branches=135, source_files=95))
            old = {entry['id']: entry for entry in self.rev10_tx['entries']}
            current = {entry['id']: entry for entry in self.tx['entries']}
            self.assertEqual(set(current) - set(old), set(REVISION11_EXPECTED['new_function_source_sha256']))
            self.assertEqual(sorted(set(current) - set(old)), review['transaction_added_function_ids'])
            self.assertEqual(set(old) - set(current), set())
            self.assertEqual(review['transaction_removed_function_ids'], [])
            self.assertEqual(self.tx['migrations'], self.rev10_tx['migrations'])
            self.assertEqual(self.tx['historical_review_metadata'], self.rev10_tx['historical_review_metadata'])
            actual_deltas = []
            for identity, before in old.items():
                after = current[identity]
                if identity == tx.PG + 'zones.go:*PgStore:CreateZone':
                    self.assertEqual(before['syntax']['sha256'], 'f02a80b8f21e9d6e2e36bbf4b4f515b01588cf53b177785f05c4bd5817b2c5bf')
                    self.assertEqual(after['syntax']['sha256'], '00b4ee15b6d00ff7453ca033833a22bed98fc56d04ace5a276df1a78c581c3c0')
                    self.assertIsNone(before.get('source_review'))
                    self.assertEqual(after['source_review']['base_commit'], SOURCE_COMMIT)
                    self.assertEqual(after['source_review']['source_sha256'], after['syntax']['sha256'])
                    for key in ('direct_sql_effects', 'direct_lock_fragments', 'transaction_helper_calls', 'sql_execution_expressions'):
                        self.assertEqual(before['assertions'][key], after['assertions'][key])
                else:
                    self.assertEqual(before['syntax']['sha256'], after['syntax']['sha256'])
                    self.assertEqual(before.get('source_review'), after.get('source_review'))
                self.assertEqual(before['classification'], after['classification'])
                self.assertEqual(before['evidence_level'], after['evidence_level'])
                for key in ('owner', 'entry', 'group', 'followup_tasks', 'review_status'):
                    self.assertEqual(before.get(key), after.get(key))
                if before != after:
                    actual_deltas.append(dict(id=identity,
                        before_sha256=hashlib.sha256(gate.canonical_bytes(before)).hexdigest(),
                        after_sha256=hashlib.sha256(gate.canonical_bytes(after)).hexdigest(),
                        changed_fields=[key for key in list(before) + [name for name in after if name not in before]
                                        if key not in before or key not in after or before[key] != after[key]]))
            self.assertEqual(actual_deltas, review['transaction_existing_entry_deltas'])
            caller_deltas = {row['id']: row for row in review['transaction_callers']}
            self.assertEqual(set(caller_deltas), {identity for identity in old if old[identity]['callers'] != current[identity]['callers']})
            for identity, delta in caller_deltas.items():
                before, after = old[identity]['callers'], current[identity]['callers']
                for side, value in [('before', before), ('after', after)]:
                    self.assertEqual(len(value), delta[side + '_count'])
                    self.assertEqual(hashlib.sha256(gate.canonical_bytes(value)).hexdigest(), delta[side + '_sha256'])
                changed = copy.deepcopy(before)
                for removed in delta['removed']:
                    changed.remove(removed)
                moved = []
                for move in delta['locations']:
                    matches = [caller for caller in changed if caller['caller_id'] == move['caller_id']
                               and caller['file'] == move['source'] and caller['expression'] == move['expression']
                               and caller['line'] == move['before_line']]
                    self.assertEqual(len(matches), 1)
                    # Consume the original location before changing it: adjacent
                    # old lines may shift onto one another's original positions.
                    changed.remove(matches[0])
                    matches[0]['line'] = move['after_line']
                    moved.append(matches[0])
                changed.extend(moved)
                changed.extend(delta['added'])
                self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in changed),
                                 Counter(json.dumps(row, sort_keys=True) for row in after))
            function_pin = review['new_function_review']
            function_raw = (ROOT / function_pin['path']).read_bytes()
            self.assertEqual(hashlib.sha256(function_raw).hexdigest(), REVISION11_EXPECTED['new_function_review_sha256'])
            self.assertEqual(function_pin['sha256'], REVISION11_EXPECTED['new_function_review_sha256'])
            functions = json.loads(function_raw)
            self.assertEqual(functions['source_commit'], REVISION11_PRIOR_PRODUCT_COMMIT)
            self.assertEqual(functions['source_tree'], '5e454667e0c99e816d20c499514d0e17f645bd22')
            self.assertFalse(functions['actual_postgres_executed_by_this_review'])
            self.assertEqual(functions['linked_test']['distinct_pg_leaves'], 78)
            self.assertEqual({row['id'] for row in functions['functions']}, set(REVISION11_EXPECTED['new_function_source_sha256']) - {tx.PG + 'zone_errors.go::classifyZoneCreateError'})
            for row in functions['functions']:
                entry = current[row['id']]
                self.assertEqual(entry['source_review'], row['source_review'])
                self.assertEqual(entry['lock_fk_wait_fence'], row['lock_fk_wait_fence'])
                self.assertEqual(entry['classification'], row['classification'])
                self.assertEqual(entry['assertions']['callback_effect'], row['callback_effect'])
                self.assertEqual(entry['syntax']['sha256'], REVISION11_EXPECTED['new_function_source_sha256'][row['id']])
                self.assertEqual(entry['evidence_level'], 'linked-existing-test')
            # The original five claim reviews stay bound to their original3a bytes.
            self.assertEqual((ROOT / (tx.PG + 'queue.go')).read_bytes(),
                             git('show', REVISION11_PRIOR_PRODUCT_COMMIT + ':' + tx.PG + 'queue.go'))
            zone_pin = review['zone_function_review']
            zone_raw = (ROOT / zone_pin['path']).read_bytes()
            self.assertEqual(hashlib.sha256(zone_raw).hexdigest(), REVISION11_EXPECTED['zone_function_review_sha256'])
            self.assertEqual(zone_pin['sha256'], REVISION11_EXPECTED['zone_function_review_sha256'])
            zone_review = json.loads(zone_raw)
            self.assertEqual(zone_review['source_commit'], SOURCE_COMMIT)
            self.assertEqual(zone_review['source_tree'], SOURCE_TREE)
            self.assertFalse(zone_review['actual_postgres_used'])
            self.assertEqual(zone_review['implementation_todos_completed'], 0)
            self.assertEqual(zone_review['linked_test']['direct_mapper_leaves'], 13)
            new_zone = tx.PG + 'zone_errors.go::classifyZoneCreateError'
            changed_zone = tx.PG + 'zones.go:*PgStore:CreateZone'
            self.assertEqual(zone_review['added_function'], new_zone)
            self.assertEqual(zone_review['changed_existing_function'], changed_zone)
            self.assertEqual(review['transaction_body_changes'], zone_review['body_changes'])
            self.assertEqual([row['id'] for row in review['transaction_body_changes']], [changed_zone])
            for row in zone_review['functions']:
                entry = current[row['id']]
                for key in ('source_review', 'classification', 'evidence_level', 'callers', 'lock_fk_wait_fence'):
                    self.assertEqual(row[key], entry[key])
                self.assertEqual(row['syntax_sha256'], entry['syntax']['sha256'])
            self.assertEqual(current[new_zone]['classification'], dict(kind='read-or-pure', direct_write=False,
                write_closure=False, explicit_lock=False, transaction_calls=False, callback_parameter=False, dynamic_sql_expression=False))
            self.assertEqual(current[changed_zone]['evidence_level'], 'source-only')
            helper = current[tx.PG + 'queue.go:*PgStore:markQueueClaim']
            self.assertEqual(helper['classification'], dict(kind='transaction-or-callback', direct_write=False,
                write_closure=False, explicit_lock=False, transaction_calls=True, callback_parameter=False, dynamic_sql_expression=True))
            self.assertEqual([row['sql_expr'] for row in helper['assertions']['sql_execution_expressions']], ['lockSQL', 'updateSQL'])
            documented = json.loads((ROOT / REVISION10_SNAPSHOTS['client_routes']['path']).read_text())
            self.assertEqual(len(documented), len(self.clients))
            indices = review['client_previous_indices']
            self.assertEqual(len(indices), len(self.clients))
            self.assertEqual(sorted(value for value in indices if value is not None), list(range(134)))
            self.assertEqual(indices[47:49], [48, 47])
            self.assertEqual([index for index, value in enumerate(indices) if value is None], [66])
            logical_keys = ('source', 'branch', 'callee', 'path', 'methods', 'forwarding')
            changed_clients, added_clients = [], []
            for index, (row, previous_index) in enumerate(zip(self.clients, indices)):
                self.assertEqual({key: value for key, value in documented[index].items() if key != 'routes'}, row)
                if previous_index is None:
                    self.assertEqual(documented[index]['routes'], ['GET /api/v1/admin/tenants/{id}'])
                    added_clients.append(dict(index=index, after=row, routes=documented[index]['routes']))
                    continue
                before = self.revision10['clients'][previous_index]
                self.assertEqual({key: before[key] for key in logical_keys}, {key: row[key] for key in logical_keys})
                self.assertEqual(documented[index]['routes'], self.revision10['client_routes'][previous_index]['routes'])
                if before != row or index != previous_index:
                    changed_clients.append(dict(index=index, previous_index=previous_index, before=before, after=row,
                        changed_fields=[key for key in before if before[key] != row[key]], routes=documented[index]['routes']))
            self.assertEqual(changed_clients, review['client_changes'])
            self.assertEqual(added_clients, review['client_added'])
            self.assertEqual(review['client_removed'], [])
            self.assertEqual(sum(row['forwarding'] for row in self.clients), 7)
            before_routes = {row['route']: row for row in self.rev10_compat['routes']}
            after_routes = {row['route']: row for row in self.compat['routes']}
            self.assertEqual(set(after_routes) - set(before_routes), {'GET /api/v1/admin/tenants/{id}'})
            self.assertTrue(set(before_routes).issubset(after_routes))
            self.assertEqual(review['compatibility_added_routes'], ['GET /api/v1/admin/tenants/{id}'])
            route_changes = []
            for row in self.compat['routes']:
                if row['route'] not in before_routes:
                    continue
                before = before_routes[row['route']]
                self.assertEqual({key: value for key, value in before.items() if key not in {'clients', 'route_line'}},
                                 {key: value for key, value in row.items() if key not in {'clients', 'route_line'}})
                if before != row:
                    route_changes.append(dict(route=row['route'], changed_fields=[key for key in before if before[key] != row[key]]))
            self.assertEqual(route_changes, review['compatibility_routes'])
            new_route = after_routes['GET /api/v1/admin/tenants/{id}']
            self.assertEqual(new_route['handler'], 'adm.GetTenantOverride')
            self.assertIn('middleware.RequireSuperAdmin', new_route['middleware'])
            self.assertEqual(new_route['schema_components'], ['TenantOverrideSnapshot'])
            self.assertEqual(new_route['go_ts_dto_bindings'], [])
            self.assertIsNone(new_route['reviewed_response_binding'])
            self.assertEqual(new_route['runtime_evidence_scope'], 'not_executed_by_this_gate')
            self.assertEqual(new_route['runtime_test_registration'], 'no_route_specific_runtime_fixture_recorded_by_this_map')
            matrix = json.loads((ROOT / 'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
            self.assertEqual(next(row for row in matrix if row['method'] == 'GET' and row['path'] == '/api/v1/admin/tenants/{id}'),
                             review['new_route_review']['original_api_matrix_row'])
            self.assertEqual((ROOT / 'docs/company-mail/evidence/R5-API-MATRIX.json').read_bytes(),
                             git('show', SOURCE_COMMIT + ':docs/company-mail/evidence/R5-API-MATRIX.json'))
            before_closure, after_closure = self.rev10_compat['source_closure'], self.compat['source_closure']
            self.assertEqual(set(after_closure) - set(before_closure), {'internal/api/handlers/admin_tenant_override.go'})
            self.assertTrue(set(before_closure).issubset(after_closure))
            self.assertEqual([dict(path=path, before_sha256=value, after_sha256=after_closure[path])
                              for path, value in before_closure.items() if value != after_closure[path]], review['closure_changes'])
            self.assertEqual([dict(path=path, sha256=value) for path, value in after_closure.items()
                              if path not in before_closure], review['closure_added'])
            for key in ('release_batches', 'historical_wire_reference', 'dependencies', 'dependency_acceptance'):
                self.assertEqual(self.compat[key], self.rev10_compat[key])
            for name, value in review['generated_catalogs'].items():
                self.assertEqual(value['path'], REVISION10_SNAPSHOTS[name]['path'])
                raw = (ROOT / value['path']).read_bytes()
                self.assertEqual(hashlib.sha256(raw).hexdigest(), value['sha256'])
                self.assertEqual(value['sha256'], REVISION11_EXPECTED['generated_catalog_sha256'][name])
                self.assertEqual(len(raw), value['bytes'])
            changed = git('diff', '--name-only', REVISION10_SOURCE_COMMIT, SOURCE_COMMIT, '--', 'internal', 'cmd', 'web').decode().splitlines()
            products = [path for path in changed if (path.endswith('.go') and not path.endswith('_test.go')) or
                        (path.endswith(('.ts', '.tsx', '.css')) and '.test.' not in path) or
                        (path.startswith('web/locales/') and path.endswith('.json'))]
            self.assertEqual(products, [row['path'] for row in review['source_changes']])
            self.assertEqual(review['excluded_closure_product_paths'], [path for path in products if path not in after_closure])
            for row in review['source_changes']:
                self.assertEqual(row['before_commit'], REVISION10_SOURCE_COMMIT)
                self.assertEqual(row['after_commit'], SOURCE_COMMIT)
                for side, commit in [('before', REVISION10_SOURCE_COMMIT), ('after', SOURCE_COMMIT)]:
                    if row[side + '_blob'] is None:
                        self.assertFalse(git('ls-tree', commit, '--', row['path']))
                        continue
                    ref = commit + ':' + row['path']
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[side + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[side + '_sha256'])
                    self.assertEqual(len(raw), row[side + '_bytes'])
                self.assertEqual((ROOT / row['path']).read_bytes(), git('show', SOURCE_COMMIT + ':' + row['path']))
            metadata = review['additional_build_metadata_changes']
            self.assertEqual([row['path'] for row in metadata], ['web/package.json', 'web/package-lock.json'])
            for row in metadata:
                self.assertEqual(row['before_commit'], REVISION11_PRIOR_PRODUCT_COMMIT)
                self.assertEqual(row['after_commit'], SOURCE_COMMIT)
                for side, commit in [('before', REVISION11_PRIOR_PRODUCT_COMMIT), ('after', SOURCE_COMMIT)]:
                    raw = git('show', commit + ':' + row['path'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[side + '_sha256'])
                    self.assertEqual(git('rev-parse', commit + ':' + row['path']).decode().strip(), row[side + '_blob'])
                    self.assertEqual(len(raw), row[side + '_bytes'])
                self.assertEqual((ROOT / row['path']).read_bytes(), git('show', SOURCE_COMMIT + ':' + row['path']))
            self.assertEqual(review['implementation_todos_completed'], 0)
            self.assertEqual(review['parent_tasks'], dict(accepted=10, total=171, remaining=161))
            for key in ('runtime_verified', 'product_green', 'task_complete'):
                self.assertFalse(review[key])

    def test_revision11_preserves_history_and_all_unapproved_rejection_guards(self):
        with self.revision11_context():
            review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
            historical = []
            for directory in review['historical_directories']:
                names = git('ls-tree', '-r', '--name-only', REVISION10_COMMIT, '--', directory).decode().splitlines()
                self.assertTrue(names)
                self.assertEqual({str(path.relative_to(ROOT)) for path in (ROOT / directory).rglob('*') if path.is_file()}, set(names))
                for path in names:
                    ref = REVISION10_COMMIT + ':' + path
                    raw = git('show', ref)
                    self.assertEqual((ROOT / path).read_bytes(), raw)
                    historical.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                           sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
            self.assertEqual(historical, review['historical_manifest'])
            self.assertEqual(len(historical), 47)
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(historical)).hexdigest(), review['historical_manifest_sha256'])
            roots = ('scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go')
            mutable = review['mutable_current_positive_test_paths']
            self.assertEqual(mutable, ['scripts/tests/test_r5_catalog_reconciliation.py',
                                      'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py'])
            paths = git('ls-tree', '-r', '--name-only', SOURCE_COMMIT, '--', *roots).decode().splitlines()
            protected = []
            for path in paths:
                if path in mutable:
                    continue
                ref = SOURCE_COMMIT + ':' + path
                raw = git('show', ref)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                protected.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(), sha256=hashlib.sha256(raw).hexdigest()))
            self.assertEqual(protected, review['protected_source_manifest'])
            self.assertEqual(len(protected), review['protected_sources'])
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(protected)).hexdigest(), review['protected_source_manifest_sha256'])
            changes = git('diff', '--name-only', REVISION10_COMMIT, SOURCE_COMMIT, '--', *roots).decode().splitlines()
            self.assertEqual(changes, [row['path'] for row in review['protected_source_changes']])
            for row in review['protected_source_changes']:
                for side, commit in [('before', REVISION10_COMMIT), ('after', SOURCE_COMMIT)]:
                    if row[side + '_blob'] is None:
                        self.assertFalse(git('ls-tree', commit, '--', row['path']))
                        continue
                    ref = commit + ':' + row['path']
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[side + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[side + '_sha256'])
            for path, digest in review['unchanged_validators_and_collectors'].items():
                raw = git('show', REVISION10_COMMIT + ':' + path)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
            # The two small current-count assertions evolve; every other byte of
            # their original validators/negative tests is retained.
            path = 'scripts/tests/test_r5_transactions.py'
            before = git('show', SOURCE_COMMIT + ':' + path).decode()
            self.assertEqual(before.count("result['functions'], 395"), 1)
            self.assertEqual(before.count("result['postgres_files'], 62"), 1)
            expected_counts = before.replace("result['functions'], 395", "result['functions'], 401")
            expected_counts = expected_counts.replace("result['postgres_files'], 62", "result['postgres_files'], 63")
            self.assertEqual((ROOT / path).read_text(), expected_counts)
            path = 'scripts/tests/test_r5_compatibility.py'
            before = git('show', SOURCE_COMMIT + ':' + path).decode()
            self.assertEqual(before.count("result['routes'],132"), 1)
            self.assertEqual(before.count("result['client_branches'],134"), 1)
            expected = before.replace("result['routes'],132", "result['routes'],133").replace("result['client_branches'],134", "result['client_branches'],135")
            self.assertEqual((ROOT / path).read_text(), expected)
            path = 'scripts/tests/test_r5_catalog_reconciliation.py'
            before_raw = git('show', REVISION10_COMMIT + ':' + path).decode()
            after_raw = (ROOT / path).read_text()
            def methods(raw):
                return {node.name: node for node in ast.walk(ast.parse(raw))
                        if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
            before, after = methods(before_raw), methods(after_raw)
            self.assertEqual((len(before), len(after)), (23, 25))
            self.assertTrue(set(before).issubset(after))
            allowed = {
                'test_old_pins_reject_and_current_revision_passes_same_actual_facts',
                'test_revision8_preserves_client_closure_history_and_all_original_guards',
                'test_revision9_preserves_all_historical_bytes_and_original_rejection_guards',
                'test_revision10_preserves_manual_reviews_and_binds_fresh_source_facts',
                'test_revision10_preserves_all_historical_bytes_and_original_rejection_guards',
            }
            for name in before:
                if name not in allowed:
                    self.assertEqual(ast.dump(before[name], include_attributes=False), ast.dump(after[name], include_attributes=False))
                    self.assertEqual(ast.get_source_segment(before_raw, before[name]), ast.get_source_segment(after_raw, after[name]))
            mutations = {name for name in before if name.startswith('test_unapproved_')}
            self.assertEqual(len(mutations), 5)
            self.assertFalse(mutations & allowed)
            for raw in (before_raw, after_raw):
                assignments = {target.id: node.value for node in ast.parse(raw).body if isinstance(node, ast.Assign)
                               for target in node.targets if isinstance(target, ast.Name)}
                if raw == before_raw:
                    historical_assignments = assignments
                else:
                    for name, node in historical_assignments.items():
                        if name.startswith('REVISION') and name.endswith(('_SNAPSHOTS', '_EXPECTED', '_COMMIT', '_TREE', '_REVIEW')):
                            self.assertEqual(ast.dump(node, include_attributes=False), ast.dump(assignments[name], include_attributes=False))

            intermediate = review['intermediate_revision11']
            self.assertEqual(intermediate['public_core_commit'], REVISION11_PRIOR_CORE_PUBLIC_COMMIT)
            self.assertEqual(intermediate['public_evidence_commit'], REVISION11_PRIOR_EVIDENCE_PUBLIC_COMMIT)
            self.assertEqual(git('rev-parse', REVISION11_PRIOR_CORE_PUBLIC_COMMIT + '^{tree}').decode().strip(),
                             'c86a86f0a252159e068c8458e343eb59248ff979')
            self.assertEqual(git('rev-parse', REVISION11_PRIOR_EVIDENCE_PUBLIC_COMMIT + '^{tree}').decode().strip(),
                             '7d1ca2357292cc35160011afe91fdbde011106c1')
            self.assertEqual(git('rev-parse', REVISION11_PRIOR_EVIDENCE_PUBLIC_COMMIT + '^').decode().strip(),
                             REVISION11_PRIOR_CORE_PUBLIC_COMMIT)
            prior_catalogs = {}
            for name, pin in intermediate['catalog_snapshots'].items():
                ref = REVISION11_PRIOR_CORE_PUBLIC_COMMIT + ':' + pin['path']
                raw = git('show', ref)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
                self.assertEqual(len(raw), pin['bytes'])
                self.assertEqual(git('rev-parse', ref).decode().strip(), pin['blob'])
                prior_catalogs[name] = json.loads(raw)
            # Only transaction facts drift in the incremental stage. Existing
            # clients/routes/scoped closure remain a positive control, not fake RED.
            with self.assertRaises(ValueError) as rejected:
                tx.validate(prior_catalogs['transaction'], self.ast, self.migrations)
            self.assertIn('zone_errors.go::classifyZoneCreateError', str(rejected.exception))
            self.assertIn('function syntax drift: ' + tx.PG + 'zones.go:*PgStore:CreateZone', str(rejected.exception))
            prior_compat = gate.validate(prior_catalogs['compatibility'], self.routes, self.clients)
            self.assertEqual(prior_compat['routes'], 133)
            self.assertEqual(prior_compat['client_branches'], 135)
            self.assertFalse(prior_compat['product_green'])
            self.assertEqual(prior_catalogs['clients'], self.clients)
            self.assertEqual(prior_catalogs['client_routes'], json.loads((ROOT / REVISION10_SNAPSHOTS['client_routes']['path']).read_text()))
            self.assertEqual(prior_catalogs['compatibility']['source_closure'], self.compat['source_closure'])
            prior_entries = {entry['id']: entry for entry in prior_catalogs['transaction']['entries']}
            current_entries = {entry['id']: entry for entry in self.tx['entries']}
            self.assertEqual(set(current_entries) - set(prior_entries), {tx.PG + 'zone_errors.go::classifyZoneCreateError'})
            changed_bodies = [identity for identity, entry in prior_entries.items()
                              if entry['syntax']['sha256'] != current_entries[identity]['syntax']['sha256']]
            self.assertEqual(changed_bodies, [tx.PG + 'zones.go:*PgStore:CreateZone'])
            for pin in intermediate['immutable_artifacts']:
                ref = REVISION11_PRIOR_EVIDENCE_PUBLIC_COMMIT + ':' + pin['path']
                raw = git('show', ref)
                self.assertEqual((ROOT / pin['path']).read_bytes(), raw)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
                self.assertEqual(len(raw), pin['bytes'])
                self.assertEqual(git('rev-parse', ref).decode().strip(), pin['blob'])
            for name, pin in intermediate['preserved_reports'].items():
                ref = REVISION11_PRIOR_EVIDENCE_PUBLIC_COMMIT + ':' + str(CURRENT_EVIDENCE.relative_to(ROOT) / name)
                raw = git('show', ref)
                self.assertEqual((ROOT / pin['path']).read_bytes(), raw)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])


    def test_revision12_binds_actual_source_facts_and_preserves_manual_reviews(self):
        with self.revision12_context():
            review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
            self.assertEqual(review['inventory_revision'], 12)
            self.assertEqual(review['source_commit'], SOURCE_COMMIT)
            self.assertEqual(review['source_tree'], SOURCE_TREE)
            self.assertEqual(git('rev-parse', SOURCE_COMMIT + '^{tree}').decode().strip(), SOURCE_TREE)
            self.assertEqual(review['previous_catalog_commit'], REVISION11_COMMIT)
            self.assertEqual(review['previous_product_source_commit'], REVISION11_SOURCE_COMMIT)
            self.assertEqual(review['revision11_snapshots'],
                             {name: dict(commit=REVISION11_COMMIT, **pin) for name, pin in REVISION11_SNAPSHOTS.items()})
            for field, expected in REVISION12_EXPECTED['review_field_sha256'].items():
                self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(), expected)
            self.assertEqual(tx.validate(self.tx, self.ast, self.migrations), review['transaction'])
            self.assertEqual(gate.validate(self.compat, self.routes, self.clients), review['compatibility'])
            self.assertEqual(review['transaction'], REVISION12_EXPECTED['transaction'])
            self.assertEqual(review['compatibility'], REVISION12_EXPECTED['compatibility'])
            self.assertEqual(self.tx['postgres_files'], self.rev11_tx['postgres_files'])
            self.assertEqual(self.tx['reviewed_file_types'], self.rev11_tx['reviewed_file_types'])
            self.assertEqual(self.tx['migrations'], self.rev11_tx['migrations'])
            self.assertEqual(self.tx['historical_review_metadata'], self.rev11_tx['historical_review_metadata'])
            old = {entry['id']: entry for entry in self.rev11_tx['entries']}
            current = {entry['id']: entry for entry in self.tx['entries']}
            self.assertEqual(set(old), set(current))
            caller_changes = []
            for identity, before in old.items():
                after = current[identity]
                self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                                 {k: v for k, v in after.items() if k != 'callers'})
                if before['callers'] != after['callers']:
                    caller_changes.append(dict(id=identity, before=before['callers'], after=after['callers'],
                        before_sha256=hashlib.sha256(gate.canonical_bytes(before['callers'])).hexdigest(),
                        after_sha256=hashlib.sha256(gate.canonical_bytes(after['callers'])).hexdigest()))
            self.assertEqual(caller_changes, review['transaction_callers'])
            self.assertEqual(review['transaction_body_changes'], [])
            self.assertEqual(review['transaction_classification_changes'], [])
            self.assertEqual(review['migration_changes'], [])
            self.assertEqual(review['client_before'], self.revision11['clients'])
            self.assertEqual(review['client_after'], self.clients)
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.revision11['clients'])).hexdigest(), review['client_before_sha256'])
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.clients)).hexdigest(), review['client_after_sha256'])
            old_routes = {row['route']: row for row in self.rev11_compat['routes']}
            new_routes = {row['route']: row for row in self.compat['routes']}
            fresh_routes = {row['route']: row for row in gate.source_facts(self.routes, self.clients)}
            self.assertEqual(set(old_routes), set(new_routes))
            route_changes = []
            for identity, before in old_routes.items():
                after = new_routes[identity]
                fact_keys = set(fresh_routes[identity])
                self.assertEqual({key: value for key, value in before.items() if key not in fact_keys},
                                 {key: value for key, value in after.items() if key not in fact_keys})
                changed = [key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)]
                if changed:
                    route_changes.append(dict(route=identity, changed_fields=changed,
                        before_sha256=hashlib.sha256(gate.canonical_bytes(before)).hexdigest(),
                        after_sha256=hashlib.sha256(gate.canonical_bytes(after)).hexdigest()))
            self.assertEqual(route_changes, review['compatibility_route_changes'])
            old_closure, new_closure = self.rev11_compat['source_closure'], self.compat['source_closure']
            closure_changes = [dict(path=path, before_sha256=old_closure.get(path), after_sha256=new_closure.get(path))
                               for path in sorted(set(old_closure) | set(new_closure)) if old_closure.get(path) != new_closure.get(path)]
            self.assertEqual(closure_changes, review['closure_changes'])
            mutable = {'inventory_revision', 'acquisition', 'routes', 'source_closure'}
            self.assertEqual({k: v for k, v in self.rev11_compat.items() if k not in mutable},
                             {k: v for k, v in self.compat.items() if k not in mutable})
            self.assertEqual(self.compat['acquisition'], dict(self.rev11_compat['acquisition'], base_commit=SOURCE_COMMIT))
            for name, pin in review['generated_catalogs'].items():
                self.assertEqual(pin['path'], REVISION11_SNAPSHOTS[name]['path'])
                raw = (ROOT / pin['path']).read_bytes()
                self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
                self.assertEqual(len(raw), pin['bytes'])
            self.assertEqual(set(review['generated_catalogs']), set(REVISION11_SNAPSHOTS))
            route_map = {(row['method'], gate.norm(row['path'])): row['method'] + ' ' + row['path'] for row in self.routes}
            documented = json.loads((ROOT / REVISION11_SNAPSHOTS['client_routes']['path']).read_text())
            self.assertEqual(documented, [dict(row, routes=[] if row['forwarding'] else
                             [route_map[(method, gate.norm(row['path']))] for method in row['methods']]) for row in self.clients])
            changed = git('diff', '--name-only', REVISION11_SOURCE_COMMIT, SOURCE_COMMIT, '--', 'internal', 'cmd', 'web').decode().splitlines()
            product_paths = [p for p in changed if (p.endswith('.go') and not p.endswith('_test.go')) or
                             (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p) or
                             (p.startswith('web/locales/') and p.endswith('.json'))]
            self.assertEqual(product_paths, [row['path'] for row in review['source_changes']])
            self.assertEqual(product_paths, REVISION12_EXPECTED['product_paths'])
            self.assertEqual(review['excluded_closure_product_paths'], [path for path in product_paths if path not in new_closure])
            build_paths = [path for path in ('web/package.json', 'web/package-lock.json')
                           if git('show', REVISION11_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
            self.assertEqual(build_paths, [row['path'] for row in review['additional_build_metadata_changes']])
            schema_paths = [path for path in ('internal/api/openapi.yaml',)
                            if git('show', REVISION11_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
            self.assertEqual(schema_paths, [row['path'] for row in review['additional_api_schema_changes']])
            for row in review['source_changes'] + review['additional_build_metadata_changes'] + review['additional_api_schema_changes']:
                self.assertEqual(row['before_commit'], REVISION11_SOURCE_COMMIT)
                self.assertEqual(row['after_commit'], SOURCE_COMMIT)
                for prefix, commit in (('before', REVISION11_SOURCE_COMMIT), ('after', SOURCE_COMMIT)):
                    if row[prefix + '_blob'] is None:
                        self.assertFalse(git('ls-tree', commit, '--', row['path']))
                        self.assertIsNone(row[prefix + '_sha256'])
                        self.assertIsNone(row[prefix + '_bytes'])
                    else:
                        ref = commit + ':' + row['path']
                        raw = git('show', ref)
                        self.assertEqual(git('rev-parse', ref).decode().strip(), row[prefix + '_blob'])
                        self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
                        self.assertEqual(len(raw), row[prefix + '_bytes'])
                self.assertEqual((ROOT / row['path']).read_bytes(), git('show', SOURCE_COMMIT + ':' + row['path']))
            self.assertFalse(review['runtime_verified'])
            self.assertFalse(review['product_green'])
            self.assertFalse(review['task_complete'])
            self.assertEqual(review['implementation_todos_completed'], 0)
            self.assertEqual(review['parent_tasks'], dict(accepted=10, total=171, remaining=161))

    def test_revision12_preserves_public_history_collectors_and_original_rejections(self):
        with self.revision12_context():
            review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
            self.assertEqual(git('rev-parse', REVISION11_COMMIT + '^{tree}').decode().strip(), REVISION11_TREE)
            historical = []
            for directory in review['historical_directories']:
                names = git('ls-tree', '-r', '--name-only', REVISION11_COMMIT, '--', directory).decode().splitlines()
                self.assertTrue(names)
                self.assertEqual({str(path.relative_to(ROOT)) for path in (ROOT / directory).rglob('*') if path.is_file()}, set(names))
                for path in names:
                    ref = REVISION11_COMMIT + ':' + path
                    raw = git('show', ref)
                    self.assertEqual((ROOT / path).read_bytes(), raw)
                    historical.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                           sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
            self.assertEqual(historical, review['historical_manifest'])
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(historical)).hexdigest(), REVISION12_EXPECTED['historical_manifest_sha256'])
            roots = ('scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go')
            mutable = ['scripts/tests/test_r5_catalog_reconciliation.py', 'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py']
            self.assertEqual(review['mutable_current_positive_test_paths'], mutable)
            paths = git('ls-tree', '-r', '--name-only', SOURCE_COMMIT, '--', *roots).decode().splitlines()
            self.assertEqual({str(path.relative_to(ROOT)) for folder in ('scripts', '.github/workflows', 'cmd/r5txinventory')
                              for path in (ROOT / folder).rglob('*') if path.is_file() and '__pycache__' not in str(path) and path.suffix != '.pyc'} |
                             {'internal/architecture/route_inventory_test.go'}, set(paths))
            protected = []
            for path in paths:
                if path in mutable:
                    continue
                ref = SOURCE_COMMIT + ':' + path
                raw = git('show', ref)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                protected.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                      sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
            self.assertEqual(protected, review['protected_source_manifest'])
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(protected)).hexdigest(), REVISION12_EXPECTED['protected_source_manifest_sha256'])
            for path, expected in review['unchanged_validators_and_collectors'].items():
                raw = git('show', REVISION11_COMMIT + ':' + path)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), expected)
            for path, replacements in (
                ('scripts/tests/test_r5_transactions.py', [("result['functions'], 401", "result['functions'], %d" % REVISION12_EXPECTED['transaction']['functions']),
                    ("result['postgres_files'], 63", "result['postgres_files'], %d" % REVISION12_EXPECTED['transaction']['postgres_files'])]),
                ('scripts/tests/test_r5_compatibility.py', [("result['routes'],133", "result['routes'],%d" % REVISION12_EXPECTED['compatibility']['routes']),
                    ("result['client_branches'],135", "result['client_branches'],%d" % REVISION12_EXPECTED['compatibility']['client_branches'])]),
            ):
                expected = git('show', REVISION11_COMMIT + ':' + path).decode()
                for before, after in replacements:
                    self.assertEqual(expected.count(before), 1)
                    expected = expected.replace(before, after)
                self.assertEqual((ROOT / path).read_text(), expected)
            path = 'scripts/tests/test_r5_catalog_reconciliation.py'
            before_raw = git('show', REVISION11_COMMIT + ':' + path).decode()
            after_raw = (ROOT / path).read_text()
            def methods(raw):
                return {node.name: node for node in ast.walk(ast.parse(raw))
                        if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
            before, after = methods(before_raw), methods(after_raw)
            self.assertEqual((len(before), len(after)), (25, 27))
            self.assertTrue(set(before).issubset(after))
            frozen = {'test_revision11_reconciles_actual_functions_clients_routes_and_source',
                      'test_revision11_preserves_history_and_all_unapproved_rejection_guards'}
            allowed = frozen | {'test_old_pins_reject_and_current_revision_passes_same_actual_facts'}
            for name in before:
                if name not in allowed:
                    self.assertEqual(ast.dump(before[name], include_attributes=False), ast.dump(after[name], include_attributes=False))
                    self.assertEqual(ast.get_source_segment(before_raw, before[name]), ast.get_source_segment(after_raw, after[name]))
            for name in frozen:
                self.assertEqual(len(after[name].body), 1)
                wrapper = after[name].body[0]
                self.assertIsInstance(wrapper, ast.With)
                self.assertEqual(ast.unparse(wrapper.items[0].context_expr), 'self.revision11_context()')
                self.assertEqual([ast.dump(node, include_attributes=False) for node in before[name].body],
                                 [ast.dump(node, include_attributes=False) for node in wrapper.body])
            guards = {name for name in before if name.startswith('test_unapproved_')}
            self.assertEqual(len(guards), 5)
            for name in guards:
                self.assertEqual(hashlib.sha256(ast.get_source_segment(after_raw, after[name]).encode()).hexdigest(),
                                 review['original_unapproved_methods'][name]['source_sha256'])
                self.assertEqual(hashlib.sha256(ast.dump(after[name], include_attributes=False).encode()).hexdigest(),
                                 review['original_unapproved_methods'][name]['ast_sha256'])
            # Fixed revision11 values and all earlier review constants are retained;
            # only the fresh root/source identifiers advance to revision12.
            def assignments(raw):
                return {target.id: node.value for node in ast.parse(raw).body if isinstance(node, ast.Assign)
                        for target in node.targets if isinstance(target, ast.Name)}
            old_values, new_values = assignments(before_raw), assignments(after_raw)
            for name, value in old_values.items():
                if name.startswith('REVISION'):
                    self.assertEqual(ast.dump(value, include_attributes=False), ast.dump(new_values[name], include_attributes=False))

    def test_revision13_binds_actual_source_facts_and_preserves_manual_reviews(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['inventory_revision'], 13)
        self.assertEqual(review['source_commit'], SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], SOURCE_TREE)
        self.assertEqual(git('rev-parse', SOURCE_COMMIT + '^{tree}').decode().strip(), SOURCE_TREE)
        self.assertEqual(review['previous_catalog_commit'], REVISION12_COMMIT)
        self.assertEqual(review['previous_product_source_commit'], REVISION12_SOURCE_COMMIT)
        self.assertEqual(review['revision12_snapshots'],
                         {name: dict(commit=REVISION12_COMMIT, **pin) for name, pin in REVISION12_SNAPSHOTS.items()})
        for field, expected in REVISION13_EXPECTED['review_field_sha256'].items():
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(), expected)
        self.assertEqual(tx.validate(self.tx, self.ast, self.migrations), review['transaction'])
        self.assertEqual(gate.validate(self.compat, self.routes, self.clients), review['compatibility'])
        self.assertEqual(review['transaction'], REVISION13_EXPECTED['transaction'])
        self.assertEqual(review['compatibility'], REVISION13_EXPECTED['compatibility'])
        actual_files = [dict(path=item['path'], sha256=item['sha256']) for item in self.ast['files'] if item['path'].startswith(tx.PG)]
        self.assertEqual(self.tx['postgres_files'], actual_files)
        before_files = {item['path']: item['sha256'] for item in self.rev12_tx['postgres_files']}
        after_files = {item['path']: item['sha256'] for item in self.tx['postgres_files']}
        self.assertEqual(set(before_files), set(after_files))
        self.assertEqual([path for path in before_files if before_files[path] != after_files[path]],
                         ['internal/store/postgres/users.go'])
        self.assertEqual(self.tx['reviewed_file_types'], self.rev12_tx['reviewed_file_types'])
        self.assertEqual(self.tx['migrations'], self.rev12_tx['migrations'])
        self.assertEqual(self.tx['historical_review_metadata'], self.rev12_tx['historical_review_metadata'])
        mutable_transaction = {'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary', 'postgres_files', 'entries'}
        self.assertEqual({key: value for key, value in self.rev12_tx.items() if key not in mutable_transaction},
                         {key: value for key, value in self.tx.items() if key not in mutable_transaction})
        functions = {item['id']: item for item in self.ast['functions'] if item['file'].startswith(tx.PG)}
        classes = tx.classify(self.ast)
        old = {entry['id']: entry for entry in self.rev12_tx['entries']}
        current = {entry['id']: entry for entry in self.tx['entries']}
        self.assertEqual(set(old), set(current))
        self.assertEqual(set(current), set(functions))
        reviewed_id = 'internal/store/postgres/users.go:*PgStore:CreateRefreshToken'
        reviewed_sha = '66e6bd4224e13273b7bf3904de303f128c1d4cfb980f1773b24ebf879198f1d0'
        fingerprint = lambda value: hashlib.sha256(gate.canonical_bytes(value)).hexdigest()
        caller_changes, body_changes, classification_changes, entry_changes = [], [], [], []
        for identity, before in old.items():
            after, function = current[identity], functions[identity]
            self.assertEqual(after['syntax'], function)
            self.assertEqual(after['classification'], classes[identity])
            actual_callers = [dict(caller_id=item['id'], file=item['file'], function=item['name'],
                line=call['line'], expression=call['expr'], status='name-match-candidate-not-dispatch-proof')
                for item in self.ast['functions'] for call in item['calls'] if call['name'] == function['name']]
            self.assertEqual(after['callers'], actual_callers)
            derived = {
                'direct_sql_effects': [dict(line=item['line'], sql_fragment=item['value']) for item in function['strings'] if tx.MUTATION.search(item['value'])],
                'direct_lock_fragments': [dict(line=item['line'], sql_fragment=item['value']) for item in function['strings'] if tx.LOCK.search(item['value'])],
                'transaction_helper_calls': [call for call in function['calls'] if call['name'] in tx.TX],
                'sql_execution_expressions': [call for call in function['calls'] if call['name'] in tx.SQL_CALLS],
            }
            self.assertEqual({key: after['assertions'][key] for key in derived}, derived)
            self.assertEqual({key: value for key, value in before['assertions'].items() if key not in derived},
                             {key: value for key, value in after['assertions'].items() if key not in derived})
            mutable = {'syntax', 'classification', 'callers', 'assertions'}
            if identity == reviewed_id:
                self.assertEqual(before['syntax']['sha256'], '84457782c35718446b19a00ba7e07022a0b58f5cf6bf96172bf8bd67da887edb')
                self.assertEqual(function['sha256'], reviewed_sha)
                self.assertNotIn('source_review', before)
                manual = ('lock_fk_wait_fence', 'evidence', 'unverified_risks', 'file_family_context')
                self.assertEqual(after['historical_revision12_review'], {key: before[key] for key in manual})
                self.assertEqual({key: after[key] for key in (*manual, 'source_review')},
                                 REVISION13_EXPECTED['refresh_manual_fields'])
                self.assertEqual(after['source_review']['base_commit'], SOURCE_COMMIT)
                self.assertEqual(after['source_review']['source_sha256'], function['sha256'])
                # Function-valued exec aliases remain outside the unchanged
                # call-name SQL collector. The explicit manual record binds it.
                self.assertTrue(any(call['name'] == 'exec' for call in function['calls']))
                self.assertFalse(any(call['name'] == 'exec' for call in derived['sql_execution_expressions']))
                self.assertEqual(after['lock_fk_wait_fence']['local_operations'],
                    [dict(line=call['line'], operation=call['expr'], sql_expression=call.get('sql_expr'), status='lexical-source-only')
                     for call in function['calls'] if call['name'] in tx.SQL_CALLS or call['name'] == 'exec'])
                mutable.update((*manual, 'historical_revision12_review', 'source_review'))
            else:
                self.assertEqual(before['syntax']['sha256'], function['sha256'])
                self.assertEqual(before['classification'], after['classification'])
                if before['syntax'] != function:
                    self.assertEqual(function['file'], 'internal/store/postgres/users.go')
            self.assertEqual({key: value for key, value in before.items() if key not in mutable},
                             {key: value for key, value in after.items() if key not in mutable})
            if before['callers'] != after['callers']:
                caller_changes.append(dict(id=identity, before=before['callers'], after=after['callers'],
                    before_sha256=fingerprint(before['callers']), after_sha256=fingerprint(after['callers'])))
            if before['syntax']['sha256'] != function['sha256']:
                body_changes.append(dict(id=identity, before_sha256=before['syntax']['sha256'], after_sha256=function['sha256']))
            if before['classification'] != after['classification']:
                classification_changes.append(dict(id=identity, before=before['classification'], after=after['classification']))
            if before != after:
                entry_changes.append(dict(id=identity,
                    changed_fields=[key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)],
                    before_sha256=fingerprint(before), after_sha256=fingerprint(after)))
        self.assertEqual(caller_changes, review['transaction_callers'])
        self.assertEqual(body_changes, review['transaction_body_changes'])
        self.assertEqual(classification_changes, review['transaction_classification_changes'])
        self.assertEqual(entry_changes, review['transaction_entry_changes'])
        self.assertEqual([row['id'] for row in body_changes], [reviewed_id])
        self.assertEqual([row['id'] for row in classification_changes], [reviewed_id])
        self.assertEqual(review['migration_changes'], [])
        self.assertEqual(review['client_before'], self.revision12['clients'])
        self.assertEqual(review['client_after'], self.clients)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.revision12['clients'])).hexdigest(), review['client_before_sha256'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.clients)).hexdigest(), review['client_after_sha256'])
        old_routes = {row['route']: row for row in self.rev12_compat['routes']}
        new_routes = {row['route']: row for row in self.compat['routes']}
        fresh_routes = {row['route']: row for row in gate.source_facts(self.routes, self.clients)}
        self.assertEqual(set(old_routes), set(new_routes))
        route_changes = []
        for identity, before in old_routes.items():
            after = new_routes[identity]
            fact_keys = set(fresh_routes[identity])
            self.assertEqual({key: value for key, value in before.items() if key not in fact_keys},
                             {key: value for key, value in after.items() if key not in fact_keys})
            changed = [key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)]
            if changed:
                route_changes.append(dict(route=identity, changed_fields=changed,
                    before_sha256=hashlib.sha256(gate.canonical_bytes(before)).hexdigest(),
                    after_sha256=hashlib.sha256(gate.canonical_bytes(after)).hexdigest()))
        self.assertEqual(route_changes, review['compatibility_route_changes'])
        old_closure, new_closure = self.rev12_compat['source_closure'], self.compat['source_closure']
        closure_changes = [dict(path=path, before_sha256=old_closure.get(path), after_sha256=new_closure.get(path))
                           for path in sorted(set(old_closure) | set(new_closure)) if old_closure.get(path) != new_closure.get(path)]
        self.assertEqual(closure_changes, review['closure_changes'])
        mutable = {'inventory_revision', 'acquisition', 'routes', 'source_closure'}
        self.assertEqual({k: v for k, v in self.rev12_compat.items() if k not in mutable},
                         {k: v for k, v in self.compat.items() if k not in mutable})
        self.assertEqual(self.compat['acquisition'], dict(self.rev12_compat['acquisition'], base_commit=SOURCE_COMMIT))
        for name, pin in review['generated_catalogs'].items():
            self.assertEqual(pin['path'], REVISION12_SNAPSHOTS[name]['path'])
            raw = (ROOT / pin['path']).read_bytes()
            self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
            self.assertEqual(len(raw), pin['bytes'])
        self.assertEqual(set(review['generated_catalogs']), set(REVISION12_SNAPSHOTS))
        route_map = {(row['method'], gate.norm(row['path'])): row['method'] + ' ' + row['path'] for row in self.routes}
        documented = json.loads((ROOT / REVISION12_SNAPSHOTS['client_routes']['path']).read_text())
        self.assertEqual(documented, [dict(row, routes=[] if row['forwarding'] else
                         [route_map[(method, gate.norm(row['path']))] for method in row['methods']]) for row in self.clients])
        changed = git('diff', '--name-only', REVISION12_SOURCE_COMMIT, SOURCE_COMMIT, '--', 'internal', 'cmd', 'web').decode().splitlines()
        product_paths = [p for p in changed if (p.endswith('.go') and not p.endswith('_test.go')) or
                         (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p) or
                         (p.startswith('web/locales/') and p.endswith('.json'))]
        self.assertEqual(product_paths, [row['path'] for row in review['source_changes']])
        self.assertEqual(product_paths, REVISION13_EXPECTED['product_paths'])
        self.assertEqual(review['excluded_closure_product_paths'], [path for path in product_paths if path not in new_closure])
        build_paths = [path for path in ('web/package.json', 'web/package-lock.json')
                       if git('show', REVISION12_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
        self.assertEqual(build_paths, [row['path'] for row in review['additional_build_metadata_changes']])
        schema_paths = [path for path in ('internal/api/openapi.yaml',)
                        if git('show', REVISION12_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
        self.assertEqual(schema_paths, [row['path'] for row in review['additional_api_schema_changes']])
        for row in review['source_changes'] + review['additional_build_metadata_changes'] + review['additional_api_schema_changes']:
            self.assertEqual(row['before_commit'], REVISION12_SOURCE_COMMIT)
            self.assertEqual(row['after_commit'], SOURCE_COMMIT)
            for prefix, commit in (('before', REVISION12_SOURCE_COMMIT), ('after', SOURCE_COMMIT)):
                if row[prefix + '_blob'] is None:
                    self.assertFalse(git('ls-tree', commit, '--', row['path']))
                    self.assertIsNone(row[prefix + '_sha256'])
                    self.assertIsNone(row[prefix + '_bytes'])
                else:
                    ref = commit + ':' + row['path']
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[prefix + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
                    self.assertEqual(len(raw), row[prefix + '_bytes'])
            self.assertEqual((ROOT / row['path']).read_bytes(), git('show', SOURCE_COMMIT + ':' + row['path']))
        self.assertFalse(review['runtime_verified'])
        self.assertFalse(review['product_green'])
        self.assertFalse(review['task_complete'])
        self.assertEqual(review['implementation_todos_completed'], 0)
        self.assertEqual(review['parent_tasks'], dict(accepted=10, total=171, remaining=161))

    def test_revision13_preserves_public_history_collectors_and_original_rejections(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(git('rev-parse', REVISION12_COMMIT + '^{tree}').decode().strip(), REVISION12_TREE)
        historical = []
        for directory in review['historical_directories']:
            names = git('ls-tree', '-r', '--name-only', REVISION12_COMMIT, '--', directory).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(path.relative_to(ROOT)) for path in (ROOT / directory).rglob('*') if path.is_file()}, set(names))
            for path in names:
                ref = REVISION12_COMMIT + ':' + path
                raw = git('show', ref)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                historical.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                       sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
        self.assertEqual(historical, review['historical_manifest'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(historical)).hexdigest(), REVISION13_EXPECTED['historical_manifest_sha256'])
        roots = ('scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go')
        mutable = ['scripts/tests/test_r5_catalog_reconciliation.py', 'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py']
        self.assertEqual(review['mutable_current_positive_test_paths'], mutable)
        paths = git('ls-tree', '-r', '--name-only', SOURCE_COMMIT, '--', *roots).decode().splitlines()
        self.assertEqual({str(path.relative_to(ROOT)) for folder in ('scripts', '.github/workflows', 'cmd/r5txinventory')
                          for path in (ROOT / folder).rglob('*') if path.is_file() and '__pycache__' not in str(path) and path.suffix != '.pyc'} |
                         {'internal/architecture/route_inventory_test.go'}, set(paths))
        protected = []
        for path in paths:
            if path in mutable:
                continue
            ref = SOURCE_COMMIT + ':' + path
            raw = git('show', ref)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            protected.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                  sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
        self.assertEqual(protected, review['protected_source_manifest'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(protected)).hexdigest(), REVISION13_EXPECTED['protected_source_manifest_sha256'])
        for path, expected in review['unchanged_validators_and_collectors'].items():
            raw = git('show', REVISION12_COMMIT + ':' + path)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), expected)
        for path, replacements in (
            ('scripts/tests/test_r5_transactions.py', [("result['functions'], 401", "result['functions'], %d" % REVISION13_EXPECTED['transaction']['functions']),
                ("result['postgres_files'], 63", "result['postgres_files'], %d" % REVISION13_EXPECTED['transaction']['postgres_files'])]),
            ('scripts/tests/test_r5_compatibility.py', [("result['routes'],133", "result['routes'],%d" % REVISION13_EXPECTED['compatibility']['routes']),
                ("result['client_branches'],136", "result['client_branches'],%d" % REVISION13_EXPECTED['compatibility']['client_branches'])]),
        ):
            expected = git('show', REVISION12_COMMIT + ':' + path).decode()
            for before, after in replacements:
                self.assertEqual(expected.count(before), 1)
                expected = expected.replace(before, after)
            self.assertEqual((ROOT / path).read_text(), expected)
        path = 'scripts/tests/test_r5_catalog_reconciliation.py'
        before_raw = git('show', REVISION12_COMMIT + ':' + path).decode()
        after_raw = (ROOT / path).read_text()
        def methods(raw):
            return {node.name: node for node in ast.walk(ast.parse(raw))
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        before, after = methods(before_raw), methods(after_raw)
        self.assertEqual((len(before), len(after)), (27, 29))
        self.assertTrue(set(before).issubset(after))
        self.assertEqual(set(after) - set(before), {'test_revision13_binds_actual_source_facts_and_preserves_manual_reviews', 'test_revision13_preserves_public_history_collectors_and_original_rejections'})
        frozen = {'test_revision12_binds_actual_source_facts_and_preserves_manual_reviews', 'test_revision12_preserves_public_history_collectors_and_original_rejections'}
        allowed = frozen | {'test_old_pins_reject_and_current_revision_passes_same_actual_facts'}
        for name in before:
            if name not in allowed:
                self.assertEqual(ast.dump(before[name], include_attributes=False), ast.dump(after[name], include_attributes=False))
                self.assertEqual(ast.get_source_segment(before_raw, before[name]), ast.get_source_segment(after_raw, after[name]))
        for name in frozen:
            self.assertEqual(len(after[name].body), 1)
            wrapper = after[name].body[0]
            self.assertIsInstance(wrapper, ast.With)
            self.assertEqual(ast.unparse(wrapper.items[0].context_expr), 'self.revision12_context()')
            self.assertEqual([ast.dump(node, include_attributes=False) for node in before[name].body],
                             [ast.dump(node, include_attributes=False) for node in wrapper.body])
            original_body = ''.join(before_raw.splitlines(keepends=True)[before[name].lineno:before[name].end_lineno])
            wrapped_body = ''.join(after_raw.splitlines(keepends=True)[after[name].lineno + 1:after[name].end_lineno])
            self.assertEqual(''.join(line[4:] if line.strip() else line for line in wrapped_body.splitlines(keepends=True)), original_body)
        guards = {name for name in before if name.startswith('test_unapproved_')}
        self.assertEqual(len(guards), 5)
        for name in guards:
            self.assertEqual(hashlib.sha256(ast.get_source_segment(after_raw, after[name]).encode()).hexdigest(),
                             review['original_unapproved_methods'][name]['source_sha256'])
            self.assertEqual(hashlib.sha256(ast.dump(after[name], include_attributes=False).encode()).hexdigest(),
                             review['original_unapproved_methods'][name]['ast_sha256'])
        # Fixed revision12 values and all earlier review constants are retained;
        # only the fresh root/source identifiers advance to revision13.
        def assignments(raw):
            return {target.id: node.value for node in ast.parse(raw).body if isinstance(node, ast.Assign)
                    for target in node.targets if isinstance(target, ast.Name)}
        old_values, new_values = assignments(before_raw), assignments(after_raw)
        for name, value in old_values.items():
            if name.startswith('REVISION'):
                self.assertEqual(ast.dump(value, include_attributes=False), ast.dump(new_values[name], include_attributes=False))
                self.assertEqual(ast.get_source_segment(before_raw, value), ast.get_source_segment(after_raw, new_values[name]))
