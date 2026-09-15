#!/usr/bin/env python3
"""Complete the assertion-checked release patch; not a runtime migrator."""
from apply import ROOT,edit,changefunc,setfunc
import json,re

# C: private default and explicit ownership, without borrowing a platform
# operator's identity across the selected company boundary.
edit('internal/app/mailboxes/service.go','type storeRepo interface {','type storeRepo interface {\n GetUser(context.Context,uuid.UUID)(*models.User,error)')
edit('internal/api/handlers/mailboxes.go','type mailboxStore interface {','type mailboxStore interface {\n GetUser(context.Context,uuid.UUID)(*models.User,error)')
edit('internal/app/mailboxes/service.go','type CreateRequest struct {','type CreateRequest struct {\n OwnerUserID *uuid.UUID')
changefunc('internal/api/handlers/mailboxes.go','func (h *MailboxHandler) Create',lambda s:s.replace('var body struct {','var body struct {\n OwnerUserID *uuid.UUID `json:"owner_user_id,omitempty"`',1).replace('Address:                body.Address,','Address:                body.Address,\n OwnerUserID: body.OwnerUserID,'))
def mailbox_create(s):
 marker='\tam := req.AccessMode'
 assert marker in s
 ownership=r'''
 ownerID:=req.OwnerUserID
 if ownerID!=nil{
  owner,err:=s.store.GetUser(ctx,*ownerID);if err!=nil{return nil,app.Internal(err)}
  if owner==nil||owner.TenantID!=tenant.ID||owner.Role!=models.RoleUser||!owner.IsActive{return nil,app.BadRequest("owner must be an active employee in this company")}
  uid:=actor.EffectiveUserID()
  if !actor.IsTenantAdmin()&&(uid==nil||*uid!=*ownerID){return nil,app.Forbidden("only a company administrator may assign another employee")}
 }else if uid:=actor.EffectiveUserID();uid!=nil{
  owner,err:=s.store.GetUser(ctx,*uid);if err!=nil{return nil,app.Internal(err)}
  if owner!=nil&&owner.IsActive&&owner.TenantID==tenant.ID{ownerID=uid}else if !actor.IsSuperAdmin{return nil,app.Forbidden("mailbox creator is not an active company member")}
 }
'''
 s=s.replace(marker,ownership+marker,1).replace('am = models.AccessPublic','am = models.AccessToken',1)
 s=s.replace('am == models.AccessToken && strings.TrimSpace(req.Password) == ""','am == models.AccessToken && ownerID == nil && strings.TrimSpace(req.Password) == ""')
 s=s.replace('password is required when access_mode=token','password is required for an ownerless token mailbox')
 s=s.replace('mb := &models.Mailbox{TenantID: tenant.ID,','mb := &models.Mailbox{OwnerUserID: ownerID,TenantID: tenant.ID,',1)
 return s
changefunc('internal/app/mailboxes/service.go','func (s *Service) Create',mailbox_create)
edit('internal/authz/authz.go','type ZoneListFilter struct {','type ZoneListFilter struct {\n // GrantedUserID is used only by mailbox queries, never zone management.\n GrantedUserID *uuid.UUID')
edit('internal/app/mailboxes/service.go','scope := authz.ZoneListScope(actor, tenant.ID)','scope := authz.ZoneListScope(actor, tenant.ID)\n if !actor.IsTenantAdmin()&&!actor.TenantWide{scope.GrantedUserID=actor.EffectiveUserID()}',2)
setfunc('internal/store/postgres/mailboxes.go','func (s *PgStore) ListMailboxesScoped',r'''func (s *PgStore) ListMailboxesScoped(ctx context.Context,scope authz.ZoneListFilter,pg models.Page)([]*models.Mailbox,int,error){
 pg=pg.Normalize()
 if !scope.AllZones&&len(scope.ZoneIDs)==0{return []*models.Mailbox{},0,nil}
 args:=[]any{scope.TenantID};clauses:=[]string{"m.tenant_id=$1"};n:=1
 if !scope.AllZones{n++;clauses=append(clauses,"m.zone_id=ANY($"+strconv.Itoa(n)+")");args=append(args,scope.ZoneIDs)}
 if scope.OwnerUserID!=nil||scope.GrantedUserID!=nil{
  rights:=[]string{}
  if scope.OwnerUserID!=nil{n++;args=append(args,*scope.OwnerUserID);rights=append(rights,"m.zone_id IN (SELECT z.id FROM domain_zones z WHERE z.tenant_id=m.tenant_id AND z.owner_user_id=$"+strconv.Itoa(n)+")")}
  if scope.GrantedUserID!=nil{n++;args=append(args,*scope.GrantedUserID);u:="$"+strconv.Itoa(n);rights=append(rights,`((m.expires_at IS NULL OR m.expires_at>clock_timestamp()) AND (m.owner_user_id=`+u+` OR EXISTS(SELECT 1 FROM mailbox_grants g WHERE g.tenant_id=m.tenant_id AND g.mailbox_id=m.id AND g.user_id=`+u+` AND g.can_read)))`)}
  clauses=append(clauses,"("+strings.Join(rights," OR ")+")")
 }
 where:=strings.Join(clauses," AND ");var total int
 if err:=s.pool.QueryRow(ctx,"SELECT count(*) FROM mailboxes m WHERE "+where,args...).Scan(&total);err!=nil{return nil,0,err}
 args=append(args,pg.PerPage,pg.Offset())
 rows,err:=s.pool.Query(ctx,mailboxSelect+" WHERE "+where+" ORDER BY m.created_at DESC,m.id LIMIT $"+strconv.Itoa(n+1)+" OFFSET $"+strconv.Itoa(n+2),args...)
 if err!=nil{return nil,0,err};defer rows.Close()
 out:=[]*models.Mailbox{}
 for rows.Next(){m,err:=s.scanMailbox(rows);if err!=nil{return nil,0,err};out=append(out,m)}
 return out,total,rows.Err()
}''')
def fake_mailbox_scope(s):
 old='''		if ownerZones != nil {
			if _, ok := ownerZones[m.ZoneID]; !ok {
				continue
			}
		}'''
 new='''
  if ownerZones!=nil||scope.GrantedUserID!=nil{
   _,visible:=ownerZones[m.ZoneID]
   if scope.GrantedUserID!=nil&&(m.ExpiresAt==nil||m.ExpiresAt.After(time.Now())){
    g:=s.mailboxGrants[[2]uuid.UUID{m.ID,*scope.GrantedUserID}]
    visible=visible||(m.OwnerUserID!=nil&&*m.OwnerUserID==*scope.GrantedUserID)||(g!=nil&&g.CanRead&&g.TenantID==scope.TenantID)
   }
   if !visible{continue}
  }'''
 assert old in s;return s.replace(old,new)
changefunc('internal/testutil/fake_store_mailboxes.go','func (s *FakeStore) ListMailboxesScoped',fake_mailbox_scope)

page='web/app/(dashboard)/console/mailboxes/page.tsx'
edit(page,'useState<AccessMode>("public")','useState<AccessMode>("token")')
edit(page,'Number.isNaN(retentionHours) || retentionHours <= 0','!Number.isInteger(retentionHours) || retentionHours < 0')
edit(page,'min="1"','min="0"')
edit(page,'placeholder={t("mailboxes.inheritDefault")}','placeholder={t("mailboxes.retentionPermanentHint")}')
edit(page,'setNewAddress("");','setNewAddress("");\n      setNewAccessMode("token");')
for lang in ['zh','en']:
 p=ROOT/f'web/locales/{lang}.json';obj=json.loads(p.read_text())
 assert 'mailboxes.retentionError' in obj
 obj['mailboxes.retentionError']='保留小时数必须为非负整数，0 表示永久保留' if lang=='zh' else 'Retention hours must be a non-negative integer; 0 means permanent retention'
 obj['mailboxes.retentionPermanentHint']='0 = 永久保留；留空继承默认值' if lang=='zh' else '0 = permanent; leave blank to inherit'
 obj['outbound.contentRedacted']='仅显示任务元数据；正文和密送信息需要发件人身份或邮箱阅读授权。' if lang=='zh' else 'Metadata only. Content and BCC require sender identity or mailbox read permission.'
 p.write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n')
p=ROOT/'web/lib/types.ts';s=p.read_text();m=re.search(r'(export (?:interface|type) OutboundJob(?:\s*=)?\s*\{)(.*?)(\n\})',s,re.S);assert m
body=m.group(2);body=re.sub(r'\n\s*delivery_token\??:[^\n]+','',body)
body='\n  content_redacted?: boolean;'+body
s=s[:m.start(2)]+body+s[m.end(2):];p.write_text(s)
p=ROOT/'web/lib/api/mailboxes.ts'
if p.exists():
 s=p.read_text();needle='address: string;'
 if needle in s:s=s.replace(needle,'address: string;\n  owner_user_id?: string;',1);p.write_text(s)

# E: company defaults and build-time standalone rewrite.
p='docker-compose.prod.yml'
edit(p,'  TABMAIL_HTTP_COOKIE_SECURE: "${TABMAIL_HTTP_COOKIE_SECURE:-true}"','''  TABMAIL_HTTP_COOKIE_SECURE: "${TABMAIL_HTTP_COOKIE_SECURE:-true}"
  TABMAIL_OPEN_REGISTRATION: "${TABMAIL_OPEN_REGISTRATION:-false}"
  TABMAIL_INGEST_DURABLE: "${TABMAIL_INGEST_DURABLE:-true}"
  TABMAIL_BOOTSTRAP_ADMIN_EMAIL: "${TABMAIL_BOOTSTRAP_ADMIN_EMAIL:-}"
  TABMAIL_BOOTSTRAP_ADMIN_PASS: "${TABMAIL_BOOTSTRAP_ADMIN_PASS:-}"
  TABMAIL_OUTBOUND_ENABLED: "${TABMAIL_OUTBOUND_ENABLED:-true}"
  TABMAIL_OUTBOUND_MODE: "${TABMAIL_OUTBOUND_MODE:-relay}"
  TABMAIL_OUTBOUND_RELAY_HOST: "${TABMAIL_OUTBOUND_RELAY_HOST:-}"
  TABMAIL_OUTBOUND_RELAY_PORT: "${TABMAIL_OUTBOUND_RELAY_PORT:-587}"
  TABMAIL_OUTBOUND_RELAY_USER: "${TABMAIL_OUTBOUND_RELAY_USER:-}"
  TABMAIL_OUTBOUND_RELAY_PASS: "${TABMAIL_OUTBOUND_RELAY_PASS:-}"
  TABMAIL_OUTBOUND_RELAY_TLS: "${TABMAIL_OUTBOUND_RELAY_TLS:-starttls}"
  TABMAIL_OUTBOUND_FROM_DOMAIN: "${TABMAIL_OUTBOUND_FROM_DOMAIN:-}"
  TABMAIL_OUTBOUND_DKIM_SIGN: "${TABMAIL_OUTBOUND_DKIM_SIGN:-true}"
  TABMAIL_OUTBOUND_DKIM_FAIL_POLICY: "${TABMAIL_OUTBOUND_DKIM_FAIL_POLICY:-fail_closed}"
  TABMAIL_OUTBOUND_REQUIRE_TLS: "${TABMAIL_OUTBOUND_REQUIRE_TLS:-true}"''')
edit(p,'  web:\n    build: ./web','''  web:
    build:
      context: ./web
      args:
        INTERNAL_API_URL: "http://tabmail-api:8080"''')
edit(p,'    image: redis:7-alpine\n    command:','    image: redis:7-alpine\n    environment:\n      REDISCLI_AUTH: "${TABMAIL_REDIS_PASSWORD:?set TABMAIL_REDIS_PASSWORD}"\n    command:')
p=ROOT/'.env.example';p.write_text(p.read_text()+'''
# --- Company-only deployment (docker-compose.prod.yml) ---
# Settings are seeded only on first start. For an existing database also turn
# open_registration off in stored system settings before exposing the service.
TABMAIL_OPEN_REGISTRATION=false
TABMAIL_OUTBOUND_ENABLED=true
TABMAIL_OUTBOUND_MODE=relay
# REQUIRED for enabled relay mode: set a real relay host before startup.
# An empty host deliberately fails config validation (no silent disabled send).
TABMAIL_OUTBOUND_RELAY_HOST=
TABMAIL_OUTBOUND_RELAY_PORT=587
TABMAIL_OUTBOUND_RELAY_USER=
TABMAIL_OUTBOUND_RELAY_PASS=
TABMAIL_OUTBOUND_RELAY_TLS=starttls
TABMAIL_OUTBOUND_FROM_DOMAIN=
TABMAIL_OUTBOUND_DKIM_SIGN=true
TABMAIL_OUTBOUND_DKIM_FAIL_POLICY=fail_closed
TABMAIL_OUTBOUND_REQUIRE_TLS=true
# Compose passes INTERNAL_API_URL as a web build argument. Rebuild the web image
# after changing that destination; runtime environment alone is insufficient.
''')
p=ROOT/'internal/api/openapi.yaml';s=p.read_text()
m=re.search(r'^    OutboundJob:\n(?:(?!^    \w).)*',s,re.M|re.S)
if m:
 block=m.group();block=re.sub(r'^        delivery_token:\n(?:^          .*\n)*','',block,flags=re.M)
 block=block.replace('      properties:\n','      properties:\n        content_redacted:\n          type: boolean\n          description: Content and BCC have been removed for a metadata-only viewer.\n',1)
 s=s[:m.start()]+block+s[m.end():]
s=s.replace('default: public','default: token')
p.write_text(s)
print('Applied C and E workspace changes')
