"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { scan, validate } = require("../collect_api_calls.cjs");
function run(source, filename="view.tsx") {
 const root=fs.mkdtempSync(path.join(os.tmpdir(),"tabmail-api-calls-"));
 try {const target=path.join(root,"web",filename);fs.mkdirSync(path.dirname(target),{recursive:true});fs.writeFileSync(target,source);return scan(root);} finally {fs.rmSync(root,{recursive:true,force:true});}
}
test("request verb and parameter identity",()=>{const r=run('request(`/api/v1/outbound/${id}`, {method:"POST"})');assert.equal(r[0].path,"/api/v1/outbound/{dynamic}");assert.deepEqual(r[0].methods,["POST"])});
test("conditional path and verb stay paired",()=>{const r=run('company(create ? "/drafts" : `/drafts/${id}`, {method:create ? "POST":"PUT"})');assert.equal(r.length,2);assert.deepEqual(r.map(x=>[x.path,x.methods]),[["/api/v1/company/drafts",["POST"]],["/api/v1/company/drafts/{dynamic}",["PUT"]]])});
test("workPath and local base expand",()=>{const r=run('const base = `${workPath(mailbox)}/messages/${id}`; company(`${base}/source`)');assert.equal(r[0].path,"/api/v1/company/mailboxes/{id}/messages/{dynamic}/source")});
test("docs strings and tests are not network calls",()=>{assert.throws(()=>run('const x="request(\"/api/v1/fake\")"'),/syntax|empty/);assert.throws(()=>run('request("/api/v1/test")',"view.test.tsx"),/empty/)});
test("opt-in audit fixture calls do not become production routes",()=>{assert.throws(()=>run('request("/fixture-only")',"permission.r5audit.tsx"),/empty/);assert.equal(run('request("/api/v1/domains")',"features/permission.tsx").length,1)});
test("source is never evaluated",()=>{const r=run('throw new Error("must not execute"); request("/api/v1/domains")');assert.equal(r.length,1)});
test("transport forwarding remains explicit",()=>{const r=run('fetch(`${getBaseUrl()}${path}`, options)',"lib/api/base.ts");assert.equal(r[0].forwarding,true)});
test("stream uses GET independent of callback options",()=>{const r=run('streamEvents("/api/v1/company/mailboxes/x/events", options)');assert.deepEqual(r[0].methods,["GET"])});
test("unrelated path and method predicates are not falsely paired",()=>{
 const r=run('company(first ? "/a":"/b", {method:second ? "POST":"PUT"})');
 assert.deepEqual(r.map(x=>x.methods), [["POST","PUT"],["POST","PUT"]]);
});
test("client drift checks reject missing/new/verb/route mismatch",()=>{
 const rows=run('request("/api/v1/domains")');const doc=rows.map(x=>({...x,routes:["GET /api/v1/domains"]}));const routes=[{method:"GET",path:"/api/v1/domains"}];
 validate(rows,doc,routes);
 assert.throws(()=>validate(rows,[],routes),/count/);
 assert.throws(()=>validate(rows,doc,[]),/unregistered/);
 assert.throws(()=>validate(rows,[{...doc[0],methods:["POST"]}],routes),/drift/);
 assert.throws(()=>validate(rows,[{...doc[0],routes:[]}],routes),/mapping/);
});
test("private helper suffixes enumerate actual routes including omitted default",()=>{
 const rows=run('function aggregate(id: string, suffix = "") { return request(`/api/v1/outbound/${encodeURIComponent(id)}${suffix}`); } aggregate(id); aggregate(id, "/recipients"); aggregate(id, "/attempts");');
 assert.deepEqual(rows.map(r=>[r.branch,r.path]),[[0,"/api/v1/outbound/{id}"],[1,"/api/v1/outbound/{id}/recipients"],[2,"/api/v1/outbound/{id}/attempts"]]);
 const routes=rows.map(r=>({method:"GET",path:r.path}));
 const doc=rows.map(r=>({...r,routes:[`GET ${r.path}`]}));
 validate(rows,doc,routes);
 assert.throws(()=>validate(rows,doc.slice(1),routes),/count/);
 assert.throws(()=>validate(rows,[...doc,doc[0]],routes),/count/);
 assert.throws(()=>validate(rows,doc,routes.slice(1)),/unregistered/);
 assert.throws(()=>validate(rows.map((r,i)=>i===1?{...r,methods:["POST"]}:r),doc,routes),/source drift/);
 assert.throws(()=>validate(rows.map((r,i)=>i===1?{...r,path:"/api/v1/outbound/{id}/unknown"}:r),doc,routes),/source drift/);
});
test("unknown, exported, escaped and spread helper suffixes stay unresolved",()=>{
 for (const source of [
  'function aggregate(id: string, suffix = "") { request(`/api/v1/outbound/${id}${suffix}`); } aggregate(id, unknown);',
  'export function aggregate(id: string, suffix = "") { request(`/api/v1/outbound/${id}${suffix}`); } aggregate(id);',
  'function aggregate(id: string, suffix = "") { request(`/api/v1/outbound/${id}${suffix}`); } aggregate(id); const escaped = aggregate;',
  'function aggregate(id: string, suffix = "") { request(`/api/v1/outbound/${id}${suffix}`); } aggregate(...args);',
 ]) {
  const rows=run(source);
  assert.equal(rows.length,1);
  assert.equal(rows[0].path,"/api/v1/outbound/{dynamic}{dynamic}");
  assert.throws(()=>validate(rows,rows.map(r=>({...r,routes:[]})),[{method:"GET",path:"/api/v1/outbound/{id}"}]),/unregistered/);
 }
});
test("recipient suffix cannot inherit the registered aggregate route",()=>{
 const rows=run('function aggregate(id: string, suffix = "") { request(`/api/v1/outbound/${encodeURIComponent(id)}${suffix}`); } aggregate(id); aggregate(id, "/recipients"); aggregate(id, "/attempts");');
 const routes=[{method:"GET",path:"/api/v1/outbound/{id}"},{method:"GET",path:"/api/v1/outbound/{id}/attempts"}];
 const doc=rows.map((r,i)=>({...r,routes:i===1?[]:[`GET ${r.path}`]}));
 assert.throws(()=>validate(rows,doc,routes),/branch 1: GET \/api\/v1\/outbound\/\{id\}\/recipients/);
 assert.throws(()=>validate(rows,[doc[0],{...doc[1],routes:doc[0].routes},doc[2]],routes),/unregistered/);
});
