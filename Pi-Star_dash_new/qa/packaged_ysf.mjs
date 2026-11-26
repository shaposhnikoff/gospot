import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {writeFileSync} from 'node:fs';

// Invoked exclusively after board_menu verifies loopback-only netns and clone marker.
export async function checkYSF({write,wd,js,click,until,ssh,root,admin,posts,cdp}){
  assert.equal(write,true);
  const paths=['/etc/MMDVM-Host.ini','/etc/DMRGateway.ini'];
  const read=p=>Buffer.from(ssh(`sudo -n base64 -w0 ${p}`),'base64').toString('latin1');
  const meta=()=>ssh(`stat -c '%u:%g %U:%G %a' ${paths.join(' ')}`);
  const units=()=>['mmdvmhost','dmrgateway'].map(u=>ssh(`systemctl is-active --quiet ${u} && systemctl show -p InvocationID --value ${u}`));
  const [host,gateway]=paths.map(read),metadata=meta();
  assert(metadata.split('\n').every(s=>s.endsWith(' mmdvm:mmdvm 644')));
  assert.match(host,/\[DMR\]\nEnable=1\n/);
  assert.match(host,/\[DMR Network\]\nEnable=1\n/);
  let count=0;
  // Independent oracle: replace a known unique literal block, never serialize the form.
  const block=(on,time)=>`[System Fusion]\nEnable=${on}\nTXHang=${time}  # fixture: transmit hang\n[System Fusion Network]\nEnable=${on}\n`;
  const base=block(0,4);
  assert.equal(host.split(base).length,2,'prepare the disabled YSF fixture before running');
  function disk(on,time){
    const want=[host.replace(base,block(+on,time)),gateway];
    for(const [i,p] of paths.entries()){
      const got=read(p);
      assert(got===want[i],`${p}: unexpected bytes`);
      console.log(`  ${p}: exact SHA256=${createHash('sha256').update(got,'latin1').digest('hex')}`);
    }
    assert.equal(meta(),metadata);
  }
  const toggle='input[name="mode-System Fusion"]',dmr='input[name="mode-DMR"]';
  const input='input[name="System Fusion.TXHang"]',card='fieldset[data-protocol="System Fusion"]';
  async function open(){
    await wd('/url',{url:root+'/'});
    const link='nav a[data-testid="open-radio-settings"]';
    await until(()=>js('return !!document.querySelector(arguments[0])',link)); await click(link);
    await until(()=>js('return !!document.querySelector(arguments[0]) && !document.querySelector(arguments[0]).matches(":disabled")',toggle));
    assert.equal(await js('return location.href'),admin+'/manage/#configuration');
    assert.equal(await js('return document.querySelector(arguments[0]).checked',dmr),true);
  }
  async function visible(on){
    assert.equal(await js('return document.querySelector(arguments[0]).checked',toggle),on);
    assert.equal(await js('return !!document.querySelector(arguments[0])?.getClientRects().length',card),on);
    assert.equal(await js('return document.querySelector(arguments[0]).checked',dmr),true);
  }
  async function save(changes){
    await click(`form:has(${toggle}) button[type=submit]`);
    await until(()=>js(`return [...document.querySelectorAll('[role=status]')].some(e=>e.innerText==='Настройки сохранены.')`));
    assert.equal(posts.length,++count);
    const post=posts.at(-1);
    assert.equal(post.path,'/manage/api/protocols');assert.equal(post.status,204);assert(post.csrf);
    assert.match(post.body.revision,/^[0-9a-f]{64}$/);
    assert.deepEqual(post.body.changes,{'System Fusion':changes});
    console.log(`  CSRF POST ${post.path} 204, changes=${JSON.stringify(changes)}; Настройки сохранены.`);
  }
  for(const [width,height,mobile,old,next] of [[1366,900,false,4,7],[390,844,true,7,9]]){
    await cdp('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile});
    await open();assert.equal(await js('return innerWidth'),width);
    await visible(false);console.log(`${width}×${height}: Configuration via operator menu; DMR active, YSF disabled and hidden.`);
    const before=units(), n=posts.length;
    await click(toggle);await visible(true);
    assert.equal(await js('return document.querySelector(arguments[0]).value',input),String(old));
    const el=await wd('/element',{using:'css selector',value:input});
    await wd('/element/'+el['element-6066-11e4-a52e-4f735466cecf']+'/value',{text:'\ue009a\ue000'+next});
    assert.equal(posts.length,n);disk(false,old);assert.deepEqual(units(),before);
    assert(await js('return document.documentElement.scrollWidth <= innerWidth+1'));
    await js('window.scrollTo(0,0)');
    const {cssContentSize}=await cdp('Page.getLayoutMetrics');
    const snap=await cdp('Page.captureScreenshot',{format:'png',captureBeyondViewport:true,
      clip:{x:0,y:0,width:width,height:Math.ceil(cssContentSize.height),scale:1}});
    writeFileSync(`qa/evidence/protocol-ysf-${width}px.png`,Buffer.from(snap.data,'base64'));
    console.log('  YSF fields visible, draft produced no POST, file or service change.');
    await save({Enable:'1','net.Enable':'1',TXHang:String(next)});
    await open();await visible(true);
    assert.equal(await js('return document.querySelector(arguments[0]).value',input),String(next));
    assert.equal(await js(`return document.querySelector('input[name="System Fusion.net.Enable"]').checked`),true);
    disk(true,next); const after=units();
    assert.notEqual(after[0],before[0]);assert.equal(after[1],before[1]);assert.equal(posts.length,count);
    console.log(`PASS ${width}×${height}: YSF RF/network enabled, TXHang=${next} after reopen; DMR and all other bytes intact; mmdvm:mmdvm 0644; only MMDVMHost restarted.`);
    if(!mobile){
      await click(toggle);await visible(false);await save({Enable:'0','net.Enable':'0'});
      await open();await visible(false);disk(false,next);
      console.log('PASS desktop disable via UI: configuration hidden, TXHang retained for next enable.');
    }
  }
}
