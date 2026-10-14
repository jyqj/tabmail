import React from 'react';
import { createRoot } from 'react-dom/client';
import { SWRConfig } from 'swr';
import PermissionsPage from '../features/company/profile-management';
import { I18nProvider, preloadLocale } from '../lib/i18n';
import { installSession, clearSessionCredentials } from '../lib/session';
import './style.css';
const tenant = '10000000-0000-4000-8000-000000000011';
const profileId = '40000000-0000-4000-8000-000000000011';
const makeProfile = (id=profileId,name='Review A') => ({id,tenant_id:tenant,name,description:'original',can_send:true,daily_send_quota:10,daily_receive_quota:11,max_mailboxes:12,max_domains:13,allowed_zone_ids:null,can_create_domains:true,can_create_routes:true,can_create_api_keys:true,revision:'9007199254740993',is_system:false,created_at:'2026-10-01T00:00:00Z',updated_at:'2026-10-01T00:00:00Z'});
const state = window.review = {auth:{level:'admin',tenantId:tenant}, profiles:[makeProfile(),makeProfile('40000000-0000-4000-8000-000000000012','Review B')],calls:[],readError:0,writeError:0,delayRead:false,delayWrite:false,pending:[],events:[],makeProfile,
 login(id='admin-a', nextTenant=tenant) {state.auth.tenantId=nextTenant; installSession('synthetic-'+crypto.randomUUID(),{id,tenant_id:nextTenant,email:'review@test.invalid',display_name:id,role:'admin'});}, logout:clearSessionCredentials,
 release() {state.pending.splice(0).forEach(fn=>fn());},
 invalidate() {state.events.forEach(c=>c.enqueue(new TextEncoder().encode('event: resync\ndata: '+JSON.stringify({tenant_id:state.auth.tenantId})+'\n\n')));}
};
const json = (data,status=200) => new Response(JSON.stringify(status===200?{data}:{error:{code:status===401?'UNAUTHORIZED':status===403?'FORBIDDEN':status===409?'CONFLICT':'INTERNAL',message:'synthetic-'+status}}),{status,headers:{'Content-Type':'application/json'}});
// No network API escapes this synthetic fixture. The shipping serializer,
// auth refresh, SWR, session guard, event consumer and Base UI remain real.
window.fetch = async (input,init={}) => {
 const path=new URL(String(input),location.origin).pathname, method=init.method||'GET';
 const body=init.body?JSON.parse(String(init.body)):null;
 state.calls.push({path,method,body});
 if(path==='/api/v1/company/events') {
  let controller;
  return new Response(new ReadableStream({start(c){controller=c;state.events.push(c);init.signal?.addEventListener('abort',()=>{state.events=state.events.filter(x=>x!==c);try{c.close()}catch{}},{once:true});},cancel(){state.events=state.events.filter(x=>x!==controller);}}),{headers:{'Content-Type':'text/event-stream'}});
 }
 if(path==='/api/v1/auth/refresh') return json(null,500);
 const result = () => {
  if(path==='/api/v1/admin/permissions' && method==='GET') return json(structuredClone(state.profiles),state.readError||200);
  if(method==='PATCH') {
   if(state.writeError) return json(null,state.writeError);
   const p=state.profiles.find(p=>path.endsWith('/'+p.id));
   if(body.expected_revision!==p.revision) return json(null,409);
   const {expected_revision,...fields}=body; Object.assign(p,fields,{revision:String(BigInt(p.revision)+1n)});return json(structuredClone(p));
  }
  return json([]);
 };
 if((method==='PATCH'&&state.delayWrite)||(path==='/api/v1/admin/permissions'&&method==='GET'&&state.delayRead)) {
  const snapshot=result();return new Promise(resolve=>state.pending.push(()=>resolve(snapshot)));
 }
 return result();
};
localStorage.setItem('tabmail-locale','en');
state.login();
await preloadLocale('en');
createRoot(document.getElementById('root')).render(<SWRConfig value={{provider:()=>new Map(),dedupingInterval:0,shouldRetryOnError:false,revalidateOnFocus:false}}><I18nProvider><PermissionsPage/></I18nProvider></SWRConfig>);
