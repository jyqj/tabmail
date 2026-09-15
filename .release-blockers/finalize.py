#!/usr/bin/env python3
from apply_company import ROOT,edit,changefunc,setfunc
import re,json,subprocess

# Serialize explicit all-session revocation with rotations' user row locks.
setfunc('internal/store/postgres/users.go','func (s *PgStore) RevokeUserRefreshTokens',r'''func(s *PgStore)RevokeUserRefreshTokens(ctx context.Context,userID uuid.UUID)error{
 tx,err:=s.pool.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
 var id uuid.UUID
 err=tx.QueryRow(ctx,`SELECT id FROM users WHERE id=$1 FOR UPDATE`,userID).Scan(&id)
 if errors.Is(err,pgx.ErrNoRows){return nil};if err!=nil{return err}
 if _,err=tx.Exec(ctx,`UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`,userID);err!=nil{return err}
 return tx.Commit(ctx)
}''')
# Member-level updates must not silently ignore failures on explicit all-device
# revocation during password changes either.
edit('internal/api/handlers/auth.go','_ = h.store.RevokeUserRefreshTokens(r.Context(), user.ID)','if err:=h.store.RevokeUserRefreshTokens(r.Context(),user.ID);err!=nil{h.logger.Error().Err(err).Msg("password change: session revocation failed");errInternal(w);return}')
# Align detail visibility with the list allowlist before considering ownership.
changefunc('internal/api/handlers/outbound.go','func (h *OutboundHandler) getAccessibleOutboundJob',lambda s:s.replace('if !canAccessOutboundJob(ctx, tenant.ID, job) {','if job!=nil&&!middleware.ActorFromContext(ctx).Permission.AllowsZone(job.ZoneID){return nil,errOutboundJobNotFound}\n if !canAccessOutboundJob(ctx, tenant.ID, job) {'))
# Fix the pre-existing escaped compose healthcheck without printing any secret.
p=ROOT/'docker-compose.prod.yml';s=p.read_text();s,n=re.subn(r'      test: \["CMD-SHELL", "redis-cli [^\n]+', '      test: ["CMD-SHELL", "redis-cli ping"]',s);assert n==1;p.write_text(s)
# Redacted content is an explicit state, not a mysteriously empty message.
edit('web/app/(dashboard)/console/outbound/page.tsx','<div className="space-y-3 py-2 text-sm">','<div className="space-y-3 py-2 text-sm">\n                {detailJob.content_redacted && <p role="status" className="text-muted-foreground">{t("outbound.contentRedacted")}</p>}')
# Existing form tests follow the corrected placeholder. Additional tests below
# use the numeric input role so they also demonstrate the old behaviour red.
p=ROOT/'web/app/(dashboard)/console/mailboxes/page.test.tsx';s=p.read_text().replace('Inherit tenant default','0 = permanent; leave blank to inherit')
assert s.rstrip().endswith('});')
s=s.rstrip()[:-3]+r'''
  it("uses a private default and accepts permanent retention", async () => {
    listMailboxesMock.mockResolvedValue({ data: [], meta: { total: 0 } });
    createMailboxMock.mockResolvedValue({ data: {} });
    render(<MailboxesPage />);
    expect(screen.getByTestId("select-root")).toHaveAttribute("data-value", "token");
    expect(screen.getByRole("spinbutton")).toHaveAttribute("min", "0");
    fireEvent.change(screen.getByPlaceholderText("mail.example.com"), { target: { value: "permanent@mail.test" } });
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(createMailboxMock).toHaveBeenCalledWith(expect.objectContaining({ access_mode: "token", retention_hours_override: 0 })));
  });
  it.each(["-1", "1.5"])("rejects invalid retention %s", async (value) => {
    listMailboxesMock.mockResolvedValue({ data: [], meta: { total: 0 } });
    render(<MailboxesPage />);
    fireEvent.change(screen.getByPlaceholderText("mail.example.com"), { target: { value: "invalid@mail.test" } });
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(createMailboxMock).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledWith("Retention hours must be a non-negative integer; 0 means permanent retention");
  });
});
''';p.write_text(s)
# Scope OpenAPI changes precisely to mailbox creation, not domain/route defaults.
p=ROOT/'internal/api/openapi.yaml';s=p.read_text();original=subprocess.check_output(['git','show','6434118298387cb85fa473a4e6d78ff35c14102e:internal/api/openapi.yaml'],text=True)
# Undo the broad placeholder update while preserving the OutboundJob schema edit.
s=s.replace('default: token','default: public')
# Record the create-mailbox request properties, whether inline or component.
# Schema inspection in CI makes any uncovered contract visible for final review.
for m in re.finditer(r'^    ([A-Za-z0-9_]*Mailbox[A-Za-z0-9_]*):\n(?:(?!^    [A-Za-z0-9_]+:).)*',s,re.M|re.S):
 block=m.group()
 if 'address:' in block and 'access_mode:' in block and ('Create' in m.group(1) or 'Request' in m.group(1)):
  block=block.replace('default: public','default: token')
  if 'owner_user_id:' not in block:
   block=block.replace('      properties:\n','      properties:\n        owner_user_id:\n          type: string\n          format: uuid\n          description: Active employee in the selected company; administrators may explicitly assign ownership.\n',1)
  s=s[:m.start()]+block+s[m.end():];break
p.write_text(s)
print('Patch complete; all operational validation remains to run')
