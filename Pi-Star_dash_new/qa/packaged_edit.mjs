import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';

export async function checkPackaged({write,wd,js,click,until,ssh,root,admin,posts,reads,cdp,dmrOnly=false}) {
  const menu=async id=>{
    await wd('/url',{url:root+'/'});
    await until(()=>js(`return !!document.querySelector('nav a[href="#console"]')`));
    await click('nav a[href="#console"]');
    const link=`[data-testid="console-item-${id}"] a`;
    await until(()=>js('return !!document.querySelector(arguments[0])',link));
    await click(link);
    if(id==='expert'){
      await until(()=>js(`return !!document.querySelector('section a[href="#expert-dmrgateway"]')`));
      await click('section a[href="#expert-dmrgateway"]');
    }
  };
  const paths=['/etc/MMDVM-Host.ini','/etc/DMRGateway.ini'];
  // Read file contents only for the explicitly isolated, synthetic station.
  const read=()=>paths.map(p=>Buffer.from(ssh(`sudo -n base64 -w0 ${p}`),'base64').toString('latin1'));
  const meta=()=>ssh(`stat -c '%u:%g %U:%G %a' ${paths.join(' ')}`);
  const ids=()=>['mmdvmhost','dmrgateway'].map(u=>ssh(`systemctl is-active --quiet ${u} && systemctl show -p InvocationID --value ${u}`));
  const expected=write?read():null, metadata=meta();
  const hash=s=>createHash('sha256').update(Buffer.from(s,'latin1')).digest('hex');
  const steps=[
    [0,'configuration','cc','1','12','ColorCode=1','ColorCode=12'],
    [1,'expert','rule0','2,91,2,91,1','2,91,2,310,1','TGRewrite0=2,91,2,91,1','TGRewrite0=2,91,2,310,1']
  ];
  const baseline=expected?.slice();
  if(dmrOnly){
    assert(write);
    assert(metadata.split('\n').every(s=>s.endsWith(' mmdvm:mmdvm 644')));
    console.log('Baseline SHA256:',baseline.map(hash).join(' '),'; metadata:',metadata.replaceAll('\n',', '));
    for(const [i,,,,,a,b] of steps)assert.equal(expected[i].split('\n').filter(s=>s===a || s===b).length,1);
  }
  async function checkField(viewport,[i,id,field,a,b,lineA,lineB]){
    const reverse=dmrOnly && expected[i].includes('\n'+lineB+'\n');
    const [old,next,from,to]=reverse?[b,a,lineB,lineA]:[a,b,lineA,lineB];
    const endpoint=i?'/manage/api/services':'/manage/api/protocols';
    const sel=`input[name="${field}"]`;
    async function open(){
      const start=reads.length;
      await menu(id);
      try {
        await until(()=>js('return !!document.querySelector(arguments[0])?.getClientRects().length',sel));
      } catch(e) {
        console.log('Editor diagnostics:',await js('return [...document.querySelectorAll("h1,h2,[role=alert]")].map(n=>n.innerText)'));
        throw e;
      }
      assert.equal(await js('return location.href'),admin+'/manage/#'+(i?'expert-dmrgateway':'configuration'));
      if(dmrOnly){
        assert.deepEqual(reads.slice(start).filter(r=>r.path===endpoint).map(r=>r.status),[200]);
        assert.equal(await js('return innerWidth'),viewport[0]);
        if(!i)assert(await js(`return !!document.querySelector('fieldset[data-protocol="DMR"] input[name=cc]')`));
      }
      console.log(`Menu → ${id}${i?' → DMRGateway':''}: ${field} visible`);
    }
    await open();
    if(i===0&&!dmrOnly){
      const modes=await js(`return [...document.querySelectorAll('input[name^="mode-"]')].map(e=>[e.name.slice(5),e.checked])`);
      console.log('MMDVMHost protocol choices:',JSON.stringify(modes));
      assert(modes.some(([m])=>m==='System Fusion'),'YSF fixture/board section missing');
      const ysf='input[name="mode-System Fusion"]', card='fieldset[data-protocol="System Fusion"]';
      const checked=await js('return document.querySelector(arguments[0]).checked',ysf);
      await click(ysf);
      assert.equal(await js('return !!document.querySelector(arguments[0])',card),!checked);
      await click(ysf);
      assert.equal(await js('return !!document.querySelector(arguments[0])',card),checked);
      assert.equal(posts.length,0);
      console.log('PASS toggle YSF opens/hides its own configurator before Apply; no POST');
    }
    if(!write)return;
    const value=()=>js('return document.querySelector(arguments[0]).value',sel);
    assert.equal(await value(),old);
    const initial=ids();
    const n=posts.length;
    const {cookies}=await cdp('Network.getCookies',{urls:[admin+endpoint]});
    const cookie=cookies.find(c=>c.name==='__Host-pistar-csrf');
    assert(cookie?.secure && cookie.httpOnly && /^[0-9a-f]{64}$/.test(cookie.value));
    const el=await wd('/element',{using:'css selector',value:sel});
    const path='/element/'+el['element-6066-11e4-a52e-4f735466cecf'];
    await wd(path+'/value',{text:'\ue009a\ue000'+next});
    await click(`form:has(${sel}) button:not([type]), form:has(${sel}) button[type=submit]`);
    await until(()=>js(`return [...document.querySelectorAll('[role=status]')].some(n=>n.innerText.includes('Настройки сохранены'))`));
    assert.equal(posts.length,n+1);
    const post=posts[n];
    assert.equal(post.path,endpoint);assert.equal(post.status,204);
    assert(post.csrf && post.csrfDigest===hash(cookie.value),'POST must use the CSRF token from the loaded session');
    assert.match(post.body.revision,/^[0-9a-f]{64}$/);
    if(field==='cc')assert.deepEqual(post.body.changes,{DMR:{ColorCode:next}});
    else assert.equal(post.body.rules[0],next);
    await open();
    assert.equal(await value(),next);
    assert.equal(posts.length,n+1,'reopen must not repeat the save');
    const needle='\n'+from+'\n';
    assert.equal(expected[i].split(needle).length,2);
    expected[i]=expected[i].replace(needle,'\n'+to+'\n');
    const actual=read();
    for(let n=0;n<2;n++){
      assert(actual[n]===expected[n],`${paths[n]} bytes mismatch`);
      console.log(`${paths[n]} expected=actual SHA256 ${hash(actual[n])}`);
    }
    assert.equal(meta(),metadata);
    const after=ids();assert.notEqual(initial[i],after[i]);assert.equal(initial[1-i],after[1-i]);
    console.log(`PASS ${field}: ${old} → ${next}, one CSRF POST 204; reopened; owner/modes ${metadata.replaceAll('\n',', ')}; only target radio service restarted`);
  }
  for(const viewport of (dmrOnly?[[1366,900,false],[390,844,true]]:[null])){
    if(viewport){
      const [width,height,mobile]=viewport;
      await cdp('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile});
      console.log(`${width}×${height}`);
    }
    for(const step of steps)await checkField(viewport,step);
  }
  if(dmrOnly){
    assert.equal(posts.length,4);
    assert(read().every((s,i)=>s===baseline[i]));assert.equal(meta(),metadata);
    console.log('PASS both editors on both viewports: original files restored byte-for-byte via UI.');
  }
  if(write&&!dmrOnly){
    await menu('configuration');
    const toggle='input[name="mode-System Fusion"]';
    await until(()=>js('return !!document.querySelector(arguments[0])',toggle));
    assert.equal(await js('return document.querySelector(arguments[0]).checked',toggle),false);
    await click(toggle);
    const field='input[name="System Fusion.TXHang"]';
    const el=await wd('/element',{using:'css selector',value:field});
    await wd('/element/'+el['element-6066-11e4-a52e-4f735466cecf']+'/value',{text:'\ue009a\ue0007'});
    await click(`form:has(${toggle}) button[type=submit]`);
    await until(()=>js(`return [...document.querySelectorAll('[role=status]')].some(n=>n.innerText.includes('Настройки сохранены'))`));
    assert.equal(posts.length,3);
    assert.equal(posts[2].status,204);assert(posts[2].csrf);
    assert.deepEqual(posts[2].body.changes,{'System Fusion':{Enable:'1','net.Enable':'1',TXHang:'7'}});
    await menu('configuration');
    await until(()=>js('return document.querySelector(arguments[0])?.value==="7"',field));
    const old='[System Fusion]\nEnable=0\nTXHang=4\n[System Fusion Network]\nEnable=0';
    assert.equal(expected[0].split(old).length,2);
    expected[0]=expected[0].replace(old,old.replaceAll('Enable=0','Enable=1').replace('TXHang=4','TXHang=7'));
    const actual=read();
    assert(actual.every((s,i)=>s===expected[i]));
    assert.equal(meta(),metadata);
    console.log('PASS YSF RF + network enabled, TXHang=7 persisted after reopen; exact INIs:',actual.map(hash));
  }
}
