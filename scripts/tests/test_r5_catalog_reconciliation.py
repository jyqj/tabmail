"""Frozen revision-1/2/3/4/5/6/7 reviews and current revision-8 facts; original validators."""
import ast
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
CURRENT_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION8-20261008'
SOURCE_COMMIT = 'e0cd175996ca4ee314d7d8b8cf836023b680346c'
REVISION7_COMMIT = 'f77c31e2da38bb926dfe6fa134eabad652e94f8c'
REVISION7_SOURCE_COMMIT = '9b12c93cb03285298267e27893f74aebe742a8a2'
SOURCE_TREE = '5307be3cf057104d1bf1529e38235bbaf0c2bcdf'
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
        cls.ast = tx.extract()
        cls.migrations = tx.migration_inventory()
        cls.routes, cls.clients = gate.collect()

    def test_old_pins_reject_and_current_revision_passes_same_actual_facts(self):
        with self.assertRaisesRegex(ValueError, 'function syntax drift:.*employee_disposition'):
            tx.validate(self.old_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev2_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev3_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev4_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev5_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev6_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev7_tx, self.ast, self.migrations)
        # The old rows fail before the closure hash check because
        # reviewed client locations changed. The original hash-only rev1->rev2
        # assertion remains verified unchanged in the fixed historical checkout.
        for old in (self.old_compat, self.rev2_compat, self.rev3_compat, self.rev4_compat):
            with self.assertRaisesRegex(ValueError, '^route/schema/client/test source drift:'):
                gate.validate(old, self.routes, self.clients)
        with self.assertRaisesRegex(ValueError, '^route/schema/client/test\\ source\\ drift:\\ GET\\ /api/v1/company/mailboxes/\\{id\\}/grants$'):
            gate.validate(self.rev5_compat, self.routes, self.clients)
        with self.assertRaisesRegex(ValueError, '^route/schema/client/test\\ source\\ drift:\\ GET\\ /api/v1/company/mailboxes/\\{id\\}/grants$'):
            gate.validate(self.rev6_compat, self.routes, self.clients)
        with self.assertRaisesRegex(ValueError, '^route/schema/client/test source drift: POST /api/v1/company/mailboxes$'):
            gate.validate(self.rev7_compat, self.routes, self.clients)
        self.assertFalse(tx.validate(self.tx, self.ast, self.migrations)['runtime_verified'])
        self.assertFalse(gate.validate(self.compat, self.routes, self.clients)['product_green'])
        for name, current in (('transaction', self.tx), ('compatibility', self.compat)):
            revision = current['inventory_revision']
            pin = REVISION7_SNAPSHOTS[name]
            self.assertEqual(revision['revision'], 8)
            self.assertEqual(revision['source_commit'], SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION7_COMMIT)
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
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['inventory_revision'], 8)
        self.assertEqual(review['source_commit'], SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], SOURCE_TREE)
        self.assertEqual(SOURCE_TREE, git('rev-parse', SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision7_source_commit'], REVISION7_SOURCE_COMMIT)
        self.assertEqual(review['revision7_source_tree'], REVISION7_SOURCE_TREE)
        self.assertEqual(review['revision7_snapshots'],
                         {name: dict(commit=REVISION7_COMMIT, **pin) for name, pin in REVISION7_SNAPSHOTS.items()})
        for field in ('transaction_callers', 'transaction_syntax', 'transaction_review_fields'):
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(),
                             REVISION8_EXPECTED[field + '_sha256'])
        old = {e['id']: e for e in self.rev7_tx['entries']}
        current = {e['id']: e for e in self.tx['entries']}
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
        files = {r['path']: r['sha256'] for r in self.tx['postgres_files']}
        self.assertEqual(set(oldfiles), set(files))
        self.assertEqual([p for p in files if files[p] != oldfiles[p]], ['internal/store/postgres/company_templates.go'])
        mutable = {'entries', 'postgres_files', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev7_tx.items() if k not in mutable},
                         {k: v for k, v in self.tx.items() if k not in mutable})
        self.assertEqual(self.tx['baseline_commit'], SOURCE_COMMIT)
        self.assertEqual(self.tx['last_review_base_commit'], SOURCE_COMMIT)
        self.assertEqual(review['sql_review']['manual_review'], REVISION8_REVIEWED_SUBJECT)
        self.assertEqual(review['sql_review']['new_sql'], 'SELECT EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND id=$2)')
        self.assertEqual(review['sql_review']['before_sql_execution_calls'], 498)
        self.assertEqual(review['sql_review']['after_sql_execution_calls'], 499)
        self.assertEqual(len(old[grant]['assertions']['sql_execution_expressions']), 3)
        self.assertEqual(len(current[grant]['assertions']['sql_execution_expressions']), 4)
        self.assertEqual(tx.validate(self.tx, self.ast, self.migrations), review['transaction'])
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['transaction']['task_complete'])

    def test_revision8_preserves_client_closure_history_and_all_original_guards(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(len(self.clients), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.clients), 7)
        self.assertEqual(review['client_previous_indices'], list(range(134)))
        for field in ('clients_added', 'clients_removed', 'client_reorder'):
            self.assertEqual(review[field], [])
        documented = json.loads((ROOT / REVISION7_SNAPSHOTS['client_routes']['path']).read_text())
        changes = []
        for index, (before, after) in enumerate(zip(self.revision7['clients'], self.clients)):
            self.assertEqual({k: v for k, v in before.items() if k != 'line'}, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            self.assertEqual(documented[index]['routes'], self.revision7['client_routes'][index]['routes'])
            if before != after:
                changes.append(dict(before_index=index, after_index=index, path=after['path'], methods=after['methods'],
                    changes={k: dict(before=before[k], after=after[k]) for k in before if before[k] != after[k]}))
        self.assertEqual(len(documented), 134)
        self.assertEqual(changes, REVISION8_EXPECTED['client_changes'])
        self.assertEqual(review['client_changes'], changes)
        self.assertEqual(self.clients[17], self.revision7['clients'][17])
        self.assertEqual(len(self.compat['routes']), 132)
        route_changes = []
        for before, after in zip(self.rev7_compat['routes'], self.compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                route_changes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual(route_changes, [{'route': 'POST /api/v1/company/mailboxes', 'changed_fields': ['clients']}])
        self.assertEqual(review['compatibility_routes'], route_changes)
        self.assertEqual(set(self.compat['source_closure']), set(self.rev7_compat['source_closure']))
        self.assertEqual(len(self.compat['source_closure']), 94)
        closure_changes = sorted(path for path, digest in self.compat['source_closure'].items()
                                 if digest != self.rev7_compat['source_closure'][path])
        self.assertEqual(closure_changes, REVISION8_EXPECTED['closure_paths'])
        self.assertEqual(review['closure_changes'], closure_changes)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.compat['source_closure'])).hexdigest(), REVISION8_EXPECTED['source_closure_sha256'])
        mutable = {'inventory_revision', 'acquisition', 'routes', 'source_closure'}
        self.assertEqual({k: v for k, v in self.rev7_compat.items() if k not in mutable},
                         {k: v for k, v in self.compat.items() if k not in mutable})
        self.assertEqual(self.compat['acquisition'], dict(self.rev7_compat['acquisition'], base_commit=SOURCE_COMMIT))
        self.assertEqual(gate.validate(self.compat, self.routes, self.clients), review['compatibility'])
        with self.assertRaisesRegex(ValueError, '^route/schema/client/test source drift: POST /api/v1/company/mailboxes$'):
            gate.validate(self.rev7_compat, self.routes, self.clients)
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
        frozen_compat['inventory_revision'] = dict(self.compat['inventory_revision'], source_commit=checkpoint['product_source_commit'])
        frozen_compat['acquisition'] = dict(self.rev7_compat['acquisition'], base_commit=checkpoint['product_source_commit'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(frozen_compat)).hexdigest(), historical['generated_catalogs']['compatibility']['sha256'])
        self.assertEqual(frozen_compat['source_closure'], self.rev7_compat['source_closure'])
        self.assertEqual(frozen_compat['routes'], self.rev7_compat['routes'])
        self.assertEqual(review['transaction_syntax'], historical['transaction_syntax'])
        self.assertEqual(review['transaction_review_fields'], historical['transaction_review_fields'])
        self.assertEqual([r['path'] for r in review['source_changes']], REVISION8_EXPECTED['source_paths'])
        for row in review['source_changes']:
            self.assertEqual(row['before_commit'], REVISION7_SOURCE_COMMIT)
            self.assertEqual(row['after_commit'], SOURCE_COMMIT)
            for prefix, commit in [('before', REVISION7_SOURCE_COMMIT), ('after', SOURCE_COMMIT)]:
                ref = commit + ':' + row['path']
                if prefix == 'before' and row['before_blob'] is None:
                    self.assertFalse(git('ls-tree', commit, '--', row['path']))
                    self.assertIsNone(row['before_sha256'])
                else:
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[prefix + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
            self.assertEqual((ROOT / row['path']).read_bytes(), git('show', SOURCE_COMMIT + ':' + row['path']))
        self.assertEqual(review['excluded_closure_product_paths'],
                         ['internal/ratelimit/sliding.go', 'web/app/(dashboard)/account/page.tsx',
                          'web/components/company/send-policy.tsx', 'web/features/mail/components/received-folder.tsx'])
        self.assertTrue(set(review['excluded_closure_product_paths']).isdisjoint(self.compat['source_closure']))
        self.assertEqual(set(review['generated_catalogs']), set(REVISION7_SNAPSHOTS))
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION7_SNAPSHOTS[name]['path'])
            self.assertEqual(hashlib.sha256((ROOT / value['path']).read_bytes()).hexdigest(), value['sha256'])
        previous = json.loads(git('show', REVISION7_COMMIT + ':' + str((REVISION7_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['unchanged_validators_and_collectors'], previous['unchanged_validators_and_collectors'])
        self.assertEqual(len(review['unchanged_validators_and_collectors']), 5)
        for path, digest in review['unchanged_validators_and_collectors'].items():
            raw = git('show', REVISION7_COMMIT + ':' + path)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
            self.assertEqual((ROOT / path).read_bytes(), raw)
        for path, digest in review['unchanged_source_runner_files'].items():
            raw = git('show', SOURCE_COMMIT + ':' + path)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
        test_path = review['unchanged_compatibility_test']['path']
        self.assertEqual(test_path, 'scripts/tests/test_r5_compatibility.py')
        self.assertEqual((ROOT / test_path).read_bytes(), git('show', REVISION7_COMMIT + ':' + test_path))
        self.assertEqual(hashlib.sha256((ROOT / test_path).read_bytes()).hexdigest(), review['unchanged_compatibility_test']['sha256'])
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
        after_raw = (ROOT / test_path).read_text()
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
