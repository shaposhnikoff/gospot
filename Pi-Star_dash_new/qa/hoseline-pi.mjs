// Real Chromium on the Pi, a temporary empty profile, muted playback, no PTT.
// Plain SSH tunnel closes after the run; no persistent debugging port.
import {spawn} from 'node:child_process';
import net from 'node:net';
import {setTimeout as delay} from 'node:timers/promises';
import assert from 'node:assert/strict';
const reserve = net.createServer(); await new Promise(r=>reserve.listen(0,'127.0.0.1',r));
const port=reserve.address().port; await new Promise(r=>reserve.close(r));
const external=process.argv.includes('--external');
const remote=`set -eu; test "$(hostname -s)" = pistar3; d=$(mktemp -d); pid=
trap 'test -z "$pid" || kill "$pid" 2>/dev/null || :; rm -rf -- "$d"' EXIT
test -z "$(ss -H -ltn sport = :9574)"
nice -n 10 chromium --remote-debugging-address=127.0.0.1 --remote-debugging-port=9574 --headless --mute-audio --no-proxy-server --disable-gpu --disable-background-networking --disable-dev-shm-usage --user-data-dir="$d" --no-first-run http://192.168.210.194:8789/hoseline.html >/dev/null 2>"$d/chromium.log" &
pid=$!; timeout 90 cat >/dev/null || true`;
const tunnel=spawn('ssh',['-T','-o','BatchMode=yes','-o','ExitOnForwardFailure=yes','-L',`${port}:127.0.0.1:9574`,'pistar3',remote],{stdio:['pipe','ignore','pipe']});
let socket, next=1; const pending=new Map();
async function eventually(fn, timeout=25000) {
  const end=Date.now()+timeout; let prev;
  do {try{const v=await fn();if(v)return v;}catch(e){prev=e} await delay(250);} while(Date.now()<end);
  throw Error('Timed out waiting for browser: '+(prev?.message||'no matching DOM'));
}
async function cdp(method,params={},sessionId) {
  const id=next++;
  const p=new Promise((resolve,reject)=>{const timer=setTimeout(()=>{pending.delete(id);reject(Error(method+' timed out'))},20000); pending.set(id,m=>{clearTimeout(timer);m.error?reject(Error(m.error.message)):resolve(m.result)})});
  socket.send(JSON.stringify({method,params,id,sessionId}));return p;
}
try {
  const origin=`http://127.0.0.1:${port}`;
  const version=await eventually(async()=>{const r=await fetch(origin+'/json/version',{signal:AbortSignal.timeout(1000)});return await r.json()});
  socket=new WebSocket(version.webSocketDebuggerUrl);
  await new Promise((r,j)=>{socket.onopen=r;socket.onerror=j});
  socket.onmessage=ev=>{const m=JSON.parse(ev.data);if(pending.has(m.id)){const cb=pending.get(m.id);pending.delete(m.id);cb(m)}};
  const {targetInfos}=await cdp('Target.getTargets');
  const {sessionId}=await cdp('Target.attachToTarget',{targetId:targetInfos.find(t=>t.type==='page').targetId,flatten:true});
  const evaluate=async (expression,sid=sessionId)=>(await cdp('Runtime.evaluate',{expression,returnByValue:true},sid)).result.value;
  try {
    await eventually(()=>evaluate(`document.querySelector('#external')?.href==='https://hose.brandmeister.network/?subscribe=91'`));
  } catch(e) {
    console.error('Pi browser diagnostic:',await evaluate(`({url: location.href,title:document.title,text:document.body?.innerText.slice(0,240)})`));
    throw e;
  }
  assert.equal(await evaluate(`document.querySelectorAll('iframe').length`),0);
  console.log('PASS pistar3 actual browser:', version.Browser, 'TG91 offered, no third-party player loaded initially');
  if(external){
    // Open the standalone official player (documented fallback) in the same test tab.
    await cdp('Page.navigate',{url:'https://hose.brandmeister.network/?subscribe=91'},sessionId);
    const state=await eventually(()=>evaluate(`document.title.toLowerCase().includes('hoseline') &&
      /player/i.test(document.body.innerText) && {title:document.title,requested:location.search,player:true}`),30000);
    console.log('PASS official player rendered:',JSON.stringify(state),'(muted, no claim of audible traffic)');
  }
}finally{socket?.close(); tunnel.stdin.end();}
