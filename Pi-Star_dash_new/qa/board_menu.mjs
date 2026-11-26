// Read-only Chromium comparison on the pilot. Run from Pi-Star_dash_new.
import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { X509Certificate, createHash } from 'node:crypto';
import { createServer } from 'node:net';
import { setTimeout as delay } from 'node:timers/promises';
import {checkPackaged} from './packaged_edit.mjs';

const board='192.168.210.194', root=`http://${board}:8789`, admin=`https://${board}:8788`;
const mode=process.argv[2];
assert([undefined,'--deb-readonly','--deb-fixture-write'].includes(mode));
const write=mode==='--deb-fixture-write';
if(write){
  assert.deepEqual(JSON.parse(execFileSync('ip',['-j','link'])).map(i=>i.ifname),['lo']);
  assert.equal(JSON.parse(execFileSync('ip',['-j','route','get',board]))[0].dev,'lo');
}
const ssh = cmd => execFileSync('ssh',['-T','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','pistar3',cmd],{encoding:'utf8'}).trim();
assert.equal(ssh('hostname -s'),'pistar3');
if(write) assert.equal(ssh('sudo -n cat /run/qa-isolated-dashboard'),'isolated synthetic radio INIs');
const radio = () => ssh('sudo -n sha256sum /etc/MMDVM-Host.ini /etc/DMRGateway.ini && systemctl show -p InvocationID --value mmdvmhost dmrgateway');
const before=radio();
const pin=createHash('sha256').update(new X509Certificate(ssh('sudo -n cat /etc/pistar-wizard/tls-cert.pem')).publicKey.export({type:'spki',format:'der'})).digest('base64');
const show = p => execFileSync('git',['-C','../Pi-Star_DV_Dash','show','10bf34c9:'+p],{encoding:'utf8'});
const en=Object.fromEntries([...show('lang/english_uk.php').matchAll(/"(\w+)"\s*=>\s*"([^"]+)"/g)].map(m=>m.slice(1)));
const cfg=show('admin/configure.php');
assert(cfg.includes('href="/admin/expert/"'));
console.log('Reference 10bf34c9 Configuration headings (including conditional):',
  [...cfg.matchAll(/<h2>[^\n]*?\$lang\['(\w+)'\][^\n]*?<\/h2>/g)].map(m=>en[m[1]]||m[1]).join(' | '));
for (const m of show('admin/expert/header-menu.inc').matchAll(/<b>(Quick Edit|Full Edit|Tools):<\/b>([\s\S]*?)(?=<b>|<\/p>)/g))
  console.log(`Reference Expert / ${m[1]}:`,[...m[2].matchAll(/<a [^>]+>([^<]+)<\/a>/g)].map(a=>a[1]).join(' | '));

const reserve=createServer(); await new Promise(r=>reserve.listen(0,'127.0.0.1',r));
const local='http://127.0.0.1:'+reserve.address().port;
await new Promise(r=>reserve.close(r));
const driver=spawn('chromedriver',['--port='+new URL(local).port],{stdio:'ignore'});
async function until(fn) {
  for(let n=0;n<100;n++){ const result=await fn(); if(result)return result; await delay(100); }
  throw Error('Browser wait timed out');
}
let session='',ws;
const wire=async (path,body,method='POST')=>{
  const res=await fetch(local+path,{method,headers:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
  if(!res.ok) throw Error(`WebDriver ${path.split('/').pop()} HTTP ${res.status}`);
  return (await res.json()).value;
};
const wd=(p,b)=>wire('/session/'+session+p,b);
const js=(script,...args)=>wd('/execute/sync',{script,args});
const click=async selector=>{
  const el=await wd('/element',{using:'css selector',value:selector});
  await wd('/element/'+el['element-6066-11e4-a52e-4f735466cecf']+'/click',{});
};
try {
  await until(()=>fetch(local+'/status').then(r=>r.ok).catch(()=>false));
  const s=await wire('/session',{capabilities:{alwaysMatch:{browserName:'chrome','goog:chromeOptions':{args:[
    '--headless=new','--disable-dev-shm-usage','--disable-background-networking','--no-first-run',
    '--proxy-server=http://127.0.0.1:1','--proxy-bypass-list='+board,'--ignore-certificate-errors-spki-list='+pin
  ]}}}});
  session=s.sessionId;
  const debug=s.capabilities['goog:chromeOptions'].debuggerAddress;
  const page=(await (await fetch('http://'+debug+'/json/list')).json()).find(t=>t.type==='page');
  ws=new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((r,j)=>{ws.onopen=r;ws.onerror=j;});
  let seq=0; const pending=new Map(), blocked=[],posts=[];
  const cdp=(method,params={})=>new Promise((r,j)=>{pending.set(++seq,{r,j});ws.send(JSON.stringify({id:seq,method,params}));});
  ws.onmessage=({data})=>{
    const {id,result,error,method,params:p}=JSON.parse(data);
    if(id){const q=pending.get(id);pending.delete(id);error?q.j(Error(error.message)):q.r(result);return;}
    if(method==='Fetch.requestPaused'){
      const u=new URL(p.request.url);
      const save=write && p.request.method==='POST' && u.href===admin+'/manage/api/services';
      const ok=(['GET','HEAD'].includes(p.request.method) && [root,admin].includes(u.origin)) || save;
      if(save)posts.push({id:p.networkId,csrf:Object.entries(p.request.headers).some(([k,v])=>k.toLowerCase()==='x-pistar-csrf'&&/^[0-9a-f]{64}$/.test(v)),body:JSON.parse(p.request.postData)});
      if(!ok)blocked.push(p.request.method+' '+u.origin+u.pathname);
      void cdp(ok?'Fetch.continueRequest':'Fetch.failRequest',ok?{requestId:p.requestId}:{requestId:p.requestId,errorReason:'BlockedByClient'});
    } else if(method==='Fetch.authRequired'){
      const a=p.authChallenge;
      const allow=a.source==='Server' && a.origin===admin && a.realm==='Pi-Star' && a.scheme==='basic' && new URL(p.request.url).pathname.startsWith('/manage/');
      void cdp('Fetch.continueWithAuth',{requestId:p.requestId,authChallengeResponse:allow?
        {response:'ProvideCredentials',username:'admin',password:ssh('sudo -n cat /etc/pistar-wizard/admin-password')}:{response:'CancelAuth'}});
    } else if(method==='Network.responseReceived' && posts.some(x=>x.id===p.requestId)){
      posts.find(x=>x.id===p.requestId).status=p.response.status;
    } else if(method==='Network.responseReceived' && p.response.url===admin+'/manage/api/services'){
      console.log('DMR settings GET HTTP',p.response.status);
    } else if(method==='Network.responseReceived' && p.type==='Document'){
      console.log(`Document ${p.response.status} ${p.response.url.split('?')[0]}`);
    } else if(method==='Network.requestWillBeSent' && p.redirectResponse && p.type==='Document'){
      console.log(`Redirect ${p.redirectResponse.status} ${p.redirectResponse.url} -> ${p.request.url}`);
    }
  };
  await cdp('Network.enable'); await cdp('Fetch.enable',{handleAuthRequests:true});
  if(mode){
    await checkPackaged({write,wd,js,click,until,ssh,root,admin,posts});
    assert.deepEqual(blocked,[]);
  }else{
  let failures=0;
  for (const [width,height,mobile] of [[1366,900,false],[390,844,true]]) {
    console.log(`\n${mobile?'Mobile':'Desktop'} ${width}x${height}`);
    await cdp('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile});
    async function menu(){
      await wd('/url',{url:root+'/'});
      await until(()=>js(`return document.querySelector('nav a[href="#console"]')?.getClientRects().length`));
      await click('nav a[href="#console"]');
      await until(()=>js(`return document.querySelector('[data-testid=console-item-expert]')?.getClientRects().length`));
      console.log('Menu: Все функции Pi-Star',await js('return location.href'));
    }
    await menu();
    const cfgLink='[data-testid=console-item-configuration] a';
    console.log('Configuration menu link:', await js('const a=document.querySelector(arguments[0]);return [a.innerText,a.href]',cfgLink));
    await click(cfgLink);
    await until(()=>js('return document.querySelector("fieldset:not([disabled]) legend")?.innerText'));
    const actual=await js(`const labels=s=>[...document.querySelectorAll(s)].filter(e=>e.getClientRects().length).map(e=>e.innerText.trim());
      return {url:location.href,menu:labels('nav a'),sections:labels('h1,h2,legend'),fields:labels('fieldset label span')}`);
    console.log('Configuration destination:',JSON.stringify(actual));
    const expertInAdmin=await js(`return [...document.querySelectorAll('nav a,header a')].some(a=>/expert|эксперт/i.test(a.innerText+' '+a.href))`);
    console.log('Expert link in configuration navigation:',expertInAdmin);
    if(!expertInAdmin || !actual.url.includes('/admin/configure.php')) {
      failures++;console.log('FAIL Configuration parity: native station editor reached; compare its sections above with reference.');
    }
    await menu();
    await click('[data-testid=console-filter-quick]');
    const expertSelector='[data-testid=console-item-expert]';
    const item=await js(`const e=document.querySelector(arguments[0]);return {label:e.innerText.trim(),active:[...e.querySelectorAll('a[href],button,[role=link]')].filter(a=>a.getClientRects().length).length}`,expertSelector);
    console.log('Expert catalog item:',JSON.stringify(item));
    if(!item.active){failures++;console.log('FAIL Expert transition: no clickable entry; no editor page reached.');}
    else {await click(expertSelector+' a');console.log('Expert destination:',await js('return [location.href,document.title]'));}
  }
  assert.deepEqual(blocked,[]);
  console.log('Read-only browser guard: GET/HEAD only, no blocked requests.');
  if(failures){console.log(`Navigation/section parity failures: ${failures}`);process.exitCode=1;}
  }
} finally {
  ws?.close();
  if(session)await wire('/session/'+session,undefined,'DELETE').catch(()=>{});
  driver.kill();
  if(!write){
    assert.equal(radio(),before);
    console.log('Before/after: radio INI checksums and mmdvmhost/dmrgateway InvocationIDs unchanged.');
  }
}
