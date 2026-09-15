from patchutil import *

p='internal/models/models.go'
replace(p,'type OutboundJob struct {','type OutboundJob struct {\n ContentRedacted bool `json:"content_redacted"`')
replace(p,'json:"delivery_token,omitempty"','json:"-"')
p='internal/authz/authz.go'
replace(p,'type ZoneListFilter struct {','type ZoneListFilter struct {\n GrantedUserID *uuid.UUID // Additional mailbox owner/read-grant branch, within the same tenant/zone boundary.')
replace(p,'type OwnerListFilter struct {','type OwnerListFilter struct {\n ReadUserID *uuid.UUID\n AllowedZoneIDs []uuid.UUID')
write('internal/authz/mailbox_scope.go',r'''
package authz
import "github.com/google/uuid"
// MailboxListScope adds mailbox relationships, without changing the domain
// management scope. Zone allowlists stay OUTSIDE the OR ownership predicate.
func MailboxListScope(a Actor,tenant uuid.UUID) ZoneListFilter {
 f:=ZoneListScope(a,tenant)
 if !a.IsTenantAdmin() && !a.TenantWide {f.GrantedUserID=a.EffectiveUserID()}
 return f
}
func OutboundListScope(a Actor,tenant uuid.UUID) OwnerListFilter {
 f:=OwnerListScope(a,tenant);f.ReadUserID=a.EffectiveUserID()
 if a.Permission!=nil {f.AllowedZoneIDs=a.Permission.AllowedZoneIDs};return f
}
''')
write('internal/api/handlers/outbound_privacy.go',r'''
package handlers
import (
 "context"
 "tabmail/internal/authz"
 "tabmail/internal/models"
)
// Every response is a copy. Redacting a list must never mutate store/cache data.
func (h *OutboundHandler) canReadOutboundContent(ctx context.Context,a authz.Actor,j *models.OutboundJob)(bool,error){
 if j==nil || a.TenantID!=j.TenantID{return false,nil}
 uid:=a.EffectiveUserID();if uid==nil{return false,nil}
 if j.SenderUserID!=nil && *uid==*j.SenderUserID{return true,nil}
 return h.canReadOutboundMailbox(ctx,a,j)
}
func(h *OutboundHandler) canReadOutboundMailbox(ctx context.Context,a authz.Actor,j *models.OutboundJob)(bool,error){
 if j==nil || a.TenantID!=j.TenantID || j.SenderMailboxID==nil || !a.Permission.AllowsZone(j.ZoneID){return false,nil}
 mb,err:=h.store.ForTenant(j.TenantID).GetMailbox(ctx,*j.SenderMailboxID);if err!=nil{return false,err}
 rights,err:=authz.MailboxRights(ctx,h.store,j.TenantID,a.EffectiveUserID(),mb)
 return rights!=nil && rights.CanRead,err
}
func(h *OutboundHandler) redactOutboundJob(ctx context.Context,a authz.Actor,j *models.OutboundJob)(*models.OutboundJob,error){
 if j==nil{return nil,nil};cp:=*j;cp.DeliveryToken=nil
 allowed,err:=h.canReadOutboundContent(ctx,a,j);if err!=nil{return nil,err}
 cp.ContentRedacted=!allowed
 if !allowed {
  cp.TextBody="";cp.HTMLBody="";cp.BCC=nil;cp.HeadersJSON=nil;cp.RawMIME=nil
  // Envelope recipients include BCC, and SMTP errors may echo those addresses.
  cp.RcptTo=append(append([]string{},j.To...),j.CC...)
  if j.LastError!=""{cp.LastError="Delivery details restricted"};cp.SMTPResponse=""
 }
 return &cp,nil
}
''')
p='internal/api/handlers/outbound.go'
replace(p,'ok(w, job)','safe, err := h.redactOutboundJob(ctx, middleware.ActorFromContext(ctx), job)\n if err != nil { errInternal(w); return }; ok(w, safe)')
replace(p,'okList(w, items, total, pg.Page, pg.PerPage)','for i, job := range items {\n  items[i], err = h.redactOutboundJob(ctx, middleware.ActorFromContext(ctx), job)\n  if err != nil { errInternal(w); return }\n }\n okList(w, items, total, pg.Page, pg.PerPage)',2)
# The second occurrence is suppression entries, not outbound! Restore that one
# by rebuilding just ListSuppressions from its original unaffected behavior.
s=read(p);start=s.index('func (h *OutboundHandler) ListSuppressions');end=s.index('\nfunc ',start+1)
part=s[start:end];part=re.sub(r'for i, job := range items \{.*?\n \}\n okList', 'okList',part,flags=re.S);Path(p).write_text(s[:start]+part+s[end:])
replace(p,'authz.OwnerListScope(middleware.ActorFromContext(ctx), tenantID)','authz.OutboundListScope(middleware.ActorFromContext(ctx), tenantID)')
replace(p,'if !canAccessOutboundJob(ctx, tenant.ID, job) {\n\t\treturn nil, errOutboundJobNotFound\n\t}',r'''if !canAccessOutboundJob(ctx, tenant.ID, job) {
  readable, readErr := h.canReadOutboundMailbox(ctx, middleware.ActorFromContext(ctx), job)
  if readErr != nil { return nil, readErr }
  if !readable { return nil, errOutboundJobNotFound }
 }''')
replace(p,'if job.State != models.OutboundDead && job.State != models.OutboundFailed {','// A read grant must not turn into authority to retry another sender\'s task.\n if !canAccessOutboundJob(ctx, tenant.ID, job) { errForbidden(w, "read access does not authorize retry"); return }\n if job.State != models.OutboundDead && job.State != models.OutboundFailed {')
replace(p,'ok(w, updatedJob)','safe, err := h.redactOutboundJob(ctx, middleware.ActorFromContext(ctx), updatedJob)\n  if err != nil { errInternal(w); return }; ok(w, safe)')
replace(p,'if _, err := h.getAccessibleOutboundJob(ctx, jobID); err != nil {','job, err := h.getAccessibleOutboundJob(ctx, jobID)\n if err != nil {')
replace(p,'ok(w, attempts)',r'''allowed, err := h.canReadOutboundContent(ctx, middleware.ActorFromContext(ctx), job)
 if err != nil { errInternal(w); return }
 if !allowed { for i, attempt := range attempts { cp := *attempt; cp.Error=""; cp.SMTPResponse=""; attempts[i]=&cp } }
 ok(w, attempts)''')
# Extend the existing SQL owner predicate only; count and rows share the WHERE.
p='internal/store/postgres/outbound.go'
s=read(p);start=s.index('func (s *PgStore) ListOutboundJobsScoped');print(s[start:start+3200])
# Add a shared-mailbox branch after the existing ownership switch.
pos=s.index('\n\tvar total int',start)
s=s[:pos]+r'''
 if scope.ReadUserID != nil {
  n++; userPlaceholder := "$"+strconv.Itoa(n); args=append(args,*scope.ReadUserID)
  shared := "sender_mailbox_id IN (SELECT m.id FROM mailboxes m WHERE m.tenant_id=$1 AND (m.expires_at IS NULL OR m.expires_at>now()) AND (m.owner_user_id="+userPlaceholder+" OR EXISTS (SELECT 1 FROM mailbox_grants g WHERE g.tenant_id=$1 AND g.mailbox_id=m.id AND g.user_id="+userPlaceholder+" AND g.can_read)))"
  if len(scope.AllowedZoneIDs)>0 { n++;args=append(args,scope.AllowedZoneIDs);shared="("+shared+" AND zone_id=ANY($"+strconv.Itoa(n)+"))" }
  // Tenant boundary is repeated inside the alternative and never OR-ed away.
  where=[]string{"("+strings.Join(where," AND ")+") OR (tenant_id=$1 AND "+shared+")"}
 }
'''+s[pos:];Path(p).write_text(s)
# Mirror relationship lookups in the stateful FakeStore, under its existing lock.
p='internal/testutil/fake_store_outbound.go'
replace(p,'switch {\n\t\tcase scope.AllInTenant:',r'''if scope.ReadUserID != nil && job.SenderMailboxID != nil && models.ZoneAllowed(scope.AllowedZoneIDs,job.ZoneID) && s.mailboxReadLocked(scope.TenantID,*job.SenderMailboxID,*scope.ReadUserID) {return true}
  switch {
  case scope.AllInTenant:''')
write('internal/testutil/fake_store_mailbox_rights.go',r'''
package testutil
import (
 "time"
 "github.com/google/uuid"
)
// Caller holds s.mu. Never call the public locking store methods from here.
func(s *FakeStore) mailboxReadLocked(tenant,mailbox,user uuid.UUID)bool{
 mb:=s.mailboxes[mailbox];if mb==nil || mb.TenantID!=tenant || (mb.ExpiresAt!=nil && !mb.ExpiresAt.After(time.Now())){return false}
 if mb.OwnerUserID!=nil && *mb.OwnerUserID==user{return true}
 g:=s.mailboxGrants[[2]uuid.UUID{mailbox,user}];return g!=nil && g.TenantID==tenant && g.CanRead
}
''')
# C: safe default, explicit ownership, and matching visible mailbox lists.
for p in ['internal/app/mailboxes/service.go','internal/api/handlers/mailboxes.go']:
 replace(p,'app.AuditStore','app.AuditStore\n GetUser(context.Context, uuid.UUID) (*models.User, error)')
p='internal/app/mailboxes/service.go'
replace(p,'type CreateRequest struct {','type CreateRequest struct {\n OwnerUserID *uuid.UUID')
replace(p,'am = models.AccessPublic','am = models.AccessToken')
replace(p,'mb := &models.Mailbox{TenantID: tenant.ID,',r'''owner := req.OwnerUserID
 if owner != nil {
  u, lookupErr := s.store.GetUser(ctx,*owner)
  if lookupErr != nil { return nil, app.Internal(lookupErr) }
  if u == nil || u.TenantID != tenant.ID || u.Role != models.RoleUser || !u.IsActive { return nil, app.BadRequest("owner must be an active ordinary member of this company") }
  // Employees cannot turn mailbox creation into an assignment API.
  self := actor.EffectiveUserID()
  if !actor.IsTenantAdmin() && !actor.TenantWide && (self==nil || *self!=*owner) {return nil,app.Forbidden("only administrators can assign another owner")}
 } else if self := actor.EffectiveUserID(); self != nil {
  u, lookupErr := s.store.GetUser(ctx,*self)
  if lookupErr != nil {return nil,app.Internal(lookupErr)}
  if u != nil && u.IsActive && u.TenantID==tenant.ID {owner=self}
  // An impersonating platform operator must not become a cross-tenant owner.
 }
 mb := &models.Mailbox{OwnerUserID: owner, TenantID: tenant.ID,''')
# Only the user-facing list gets grant visibility; creation quota remains owned-resource based.
s=read(p);s=s.replace('scope := authz.ZoneListScope(actor, tenant.ID)','scope := authz.MailboxListScope(actor, tenant.ID)',1);Path(p).write_text(s)
p='internal/api/handlers/mailboxes.go'
replace(p,'Address                string            `json:"address"`','OwnerUserID *uuid.UUID `json:"owner_user_id,omitempty"`\n  Address                string            `json:"address"`')
replace(p,'Address:                body.Address,','OwnerUserID: body.OwnerUserID,\n  Address:                body.Address,')
p='internal/store/postgres/mailboxes.go'
s=read(p);old='countClauses = append(countClauses, "zone_id IN (SELECT id FROM domain_zones WHERE owner_user_id = $"+strconv.Itoa(n)+")")\n\t\trowClauses = append(rowClauses, "m.zone_id IN (SELECT id FROM domain_zones WHERE owner_user_id = $"+strconv.Itoa(n)+")")'
assert old in s
s=s.replace(old,r'''owner := "zone_id IN (SELECT id FROM domain_zones WHERE tenant_id=$1 AND owner_user_id = $"+strconv.Itoa(n)+")"
  rowOwner := "m."+owner
  if scope.GrantedUserID != nil {
   args=append(args,*scope.OwnerUserID);n++;args=append(args,*scope.GrantedUserID)
   relation := "(expires_at IS NULL OR expires_at>now()) AND (owner_user_id=$"+strconv.Itoa(n)+" OR id IN (SELECT mailbox_id FROM mailbox_grants WHERE tenant_id=$1 AND user_id=$"+strconv.Itoa(n)+" AND can_read))"
   rowRelation := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(relation,"expires_at","m.expires_at"),"owner_user_id","m.owner_user_id")," OR id IN"," OR m.id IN")
   countClauses=append(countClauses,"("+owner+" OR ("+relation+"))")
   rowClauses=append(rowClauses,"("+rowOwner+" OR ("+rowRelation+"))")
  } else {
   countClauses=append(countClauses,owner);rowClauses=append(rowClauses,rowOwner)
   args=append(args,*scope.OwnerUserID)
  }''')
s=s.replace('\n\t\targs = append(args, *scope.OwnerUserID)','');Path(p).write_text(s)
p='internal/testutil/fake_store_mailboxes.go'
replace(p,'if _, ok := ownerZones[m.ZoneID]; !ok {\n\t\t\t\tcontinue\n\t\t\t}',r'''if _, ok := ownerZones[m.ZoneID]; !ok {
    if scope.GrantedUserID==nil || !s.mailboxReadLocked(scope.TenantID,m.ID,*scope.GrantedUserID) {continue}
   }''')
# Existing narrow test double is for delete/list behavior, not ownership provisioning.
p='internal/app/mailboxes/service_test.go'
Path(p).write_text(read(p)+'\nfunc(s *mailboxTestStore) GetUser(context.Context,uuid.UUID)(*models.User,error){return nil,nil}\n')

# TS fields and create request contract.
p='web/lib/types.ts'
s=read(p);s=re.sub(r'^\s*delivery_token\??:[^\n]*\n','\n',s,flags=re.M)
s=s.replace('export interface OutboundJob {','export interface OutboundJob {\n  content_redacted?: boolean;');Path(p).write_text(s)
# Owner input is optional for API callers; the UI defaults to the current user.
for p in Path('web/lib/api').glob('*.ts'):
 s=p.read_text()
 if 'retention_hours_override?: number' in s and 'createMailbox' in s:
  s=s.replace('retention_hours_override?: number','owner_user_id?: string;\n  retention_hours_override?: number');p.write_text(s)
p='web/app/(dashboard)/console/mailboxes/page.tsx'
s=read(p);s=s.replace('useState<AccessMode>("public")','useState<AccessMode>("token")').replace('setAccessMode("public")','setAccessMode("token")')
s=s.replace('retentionHours <= 0','retentionHours < 0').replace('min="1"','min="0"')
# Other revisions name the local parsed value retention, so match its exact guard as well.
s=re.sub(r'(retention\w*) <= 0',r'\1 < 0',s)
s=s.replace('placeholder={t("mailboxes.retentionPlaceholder")}','placeholder={t("mailboxes.retentionPermanentHint")}')
Path(p).write_text(s)
# Locale modules use nested mailboxes objects. Find the key, retain formatting.
for p in Path('web').rglob('*.ts'):
 if 'node_modules' in p.parts:continue
 s=p.read_text()
 if 'retentionError:' in s:
  chinese=bool(re.search('[\u4e00-\u9fff]',re.search(r'retentionError:[^\n]*',s).group()))
  hint='0 = 永久保留；留空继承默认值' if chinese else '0 = permanent; empty inherits default'
  msg='留存小时数必须是非负整数（0 表示永久保留）' if chinese else 'Retention hours must be a non-negative integer (0 means permanent)'
  s=re.sub(r'retentionError:\s*"[^"\n]*",',f'retentionError: "{msg}",\n    retentionPermanentHint: "{hint}",',s)
  p.write_text(s)
# Update the existing placeholder assertion and add acceptance/rejection coverage.
p='web/app/(dashboard)/console/mailboxes/page.test.tsx'
s=read(p).replace('"Inherit tenant default"','"0 = permanent; empty inherits default"')
pos=s.rfind('\n});');assert pos!=-1
s=s[:pos]+r'''
  it.each([0, -1])("private default and retention boundary %s", async (hours) => {
    listMailboxesMock.mockResolvedValue({data: [], meta: {total: 0}});
    createMailboxMock.mockResolvedValue({data: {}});
    render(<MailboxesPage />);
    expect(screen.getByTestId("select-root")).toHaveAttribute("data-value", "token");
    fireEvent.change(screen.getByPlaceholderText("mail.example.com"), {target: {value: "private@mail.test"}});
    fireEvent.change(screen.getByPlaceholderText("Enter mailbox password"), {target: {value: "Passw0rd!"}});
    const retention = screen.getByPlaceholderText("0 = permanent; empty inherits default");
    expect(retention).toHaveAttribute("min", "0");
    fireEvent.change(retention, {target: {value: String(hours)}});
    fireEvent.click(screen.getByRole("button", {name: "Create"}));
    if (hours === 0) {
      await waitFor(() => expect(createMailboxMock).toHaveBeenCalledWith(expect.objectContaining({access_mode: "token", retention_hours_override: 0})));
    } else {
      expect(createMailboxMock).not.toHaveBeenCalled();
      expect(toastError).toHaveBeenCalled();
    }
  });
'''+s[pos:];Path(p).write_text(s)

p='docker-compose.prod.yml'
replace(p,'  TABMAIL_LOGLEVEL:',r'''  TABMAIL_OPEN_REGISTRATION: "${TABMAIL_OPEN_REGISTRATION:-false}"
  TABMAIL_OUTBOUND_ENABLED: "${TABMAIL_OUTBOUND_ENABLED:-true}"
  TABMAIL_OUTBOUND_MODE: "${TABMAIL_OUTBOUND_MODE:-relay}"
  TABMAIL_OUTBOUND_RELAY_HOST: "${TABMAIL_OUTBOUND_RELAY_HOST:-}"
  TABMAIL_OUTBOUND_RELAY_PORT: "${TABMAIL_OUTBOUND_RELAY_PORT:-587}"
  TABMAIL_OUTBOUND_RELAY_USER: "${TABMAIL_OUTBOUND_RELAY_USER:-}"
  TABMAIL_OUTBOUND_RELAY_PASS: "${TABMAIL_OUTBOUND_RELAY_PASS:-}"
  TABMAIL_OUTBOUND_RELAY_TLS: "${TABMAIL_OUTBOUND_RELAY_TLS:-starttls}"
  TABMAIL_OUTBOUND_FROM_DOMAIN: "${TABMAIL_OUTBOUND_FROM_DOMAIN:-}"
  TABMAIL_LOGLEVEL:''')
replace(p,'    build: ./web','    build:\n      context: ./web\n      args:\n        INTERNAL_API_URL: "http://tabmail-api:8080"')
p='.env.example'
s=read(p)
values={'TABMAIL_OPEN_REGISTRATION':'false','TABMAIL_OUTBOUND_ENABLED':'true','TABMAIL_OUTBOUND_MODE':'relay','TABMAIL_OUTBOUND_RELAY_HOST':'smtp.example.com','TABMAIL_OUTBOUND_RELAY_PORT':'587','TABMAIL_OUTBOUND_RELAY_USER':'','TABMAIL_OUTBOUND_RELAY_PASS':'','TABMAIL_OUTBOUND_RELAY_TLS':'starttls','TABMAIL_OUTBOUND_FROM_DOMAIN':''}
for k,v in values.items():
 if re.search(r'^'+k+'=',s,re.M):s=re.sub(r'^'+k+'=.*$',k+'='+v,s,flags=re.M)
 else:s+='\n'+k+'='+v
s+='\n\n# Company deployment: public registration is disabled; outbound relay is enabled.\n# Replace smtp.example.com and set relay credentials before starting.\n# Empty relay host fails startup when enabled in relay mode. Use OUTBOUND_ENABLED=false for receive-only.\n# Existing DB-backed open_registration settings are not overwritten by env seeding: inspect them on upgrade.\n'
Path(p).write_text(s)
# Keep normal, read-only CI on main and PRs; temporary publication tooling is not copied.
p='.github/workflows/company-p0.yml'
s=read(p).replace('branches: [feat/company-mail-p0]','branches: [main, fix/company-release-blockers-r2]')
Path(p).write_text(s)
# Source-level OpenAPI contract corrections, retaining existing schema layout.
p='internal/api/openapi.yaml';s=read(p)
# Do not leave a credential described as a public response field.
s=re.sub(r'^        delivery_token:\n(?:          [^\n]*\n)+','',s,flags=re.M)
# Owner and redaction semantics are also documented in the release note.
Path(p).write_text(s)
