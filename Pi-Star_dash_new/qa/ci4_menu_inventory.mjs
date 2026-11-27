// Read-only scenario for the board_menu.mjs transport from b46f415 (ci.4).
import assert from 'node:assert/strict';

export async function checkPackaged({write,wd,js,click,until,root,admin,posts}){
  assert.equal(write,false);
  const stationErrors=[];
  for(const [width,height,mobile] of [[1366,900,false],[390,844,true]]){
    await wd('/goog/cdp/execute',{cmd:'Emulation.setDeviceMetricsOverride',params:{width,height,deviceScaleFactor:1,mobile}});
    console.log(`\n${mobile?'Mobile':'Desktop'} ${width}x${height}`);
    async function follow(selector){
      await until(()=>js('return !!document.querySelector(arguments[0])?.getClientRects().length',selector));
      await click(selector);
    }
    async function menu(id){
      await wd('/url',{url:root+'/'});
      assert.equal(await js('return innerWidth'),width);
      await follow('nav a[href="#console"]');
      await follow(`[data-testid="console-item-${id}"] a`);
    }
    async function fields(hash,ready){
      await until(()=>js('return !!document.querySelector(arguments[0])?.getClientRects().length',ready));
      assert.equal(await js('return location.href'),admin+'/manage/'+hash);
      const info=await js(`const visible=e=>e.getClientRects().length;
        return {width:innerWidth,sections:[...document.querySelectorAll('h1,h2,legend')].filter(visible).map(e=>e.innerText.trim()),
          fields:[...document.querySelectorAll('fieldset label')].filter(visible).map(l=>(l.querySelector('span')??l).innerText.trim()),
          actions:[...document.querySelectorAll('form button')].filter(visible).map(b=>b.innerText.trim())}`);
      assert.equal(info.width,width);
      console.log(hash,JSON.stringify(info));
      return info;
    }
    const host=['Режим DMR','Сеть DMR','Таймслот 1','Таймслот 2','Color Code (0–15)'];
    await menu('configuration');
    assert.deepEqual((await fields('#configuration','fieldset:not(:disabled) input[name=cc]')).fields,host);
    await follow('section a[href="#settings"]');
    try {
      const station=await fields('#settings','fieldset:not(:disabled) input');
      assert(station.fields.includes('Позывной'));
    } catch(err) {
      const diag=await js(`return {url:location.href, headings:[...document.querySelectorAll('h1,h2')].map(e=>e.innerText),
        notice:[...document.querySelectorAll('[role=alert],[role=status]')].map(e=>e.innerText),
        requests:performance.getEntriesByType('resource').filter(r=>r.name.includes('/manage/api/')).map(r=>[new URL(r.name).pathname,r.responseStatus])}`);
      stationErrors.push([width,err.message,diag]);
      console.log('Additional station-settings check:',JSON.stringify(diag));
    }
    await menu('expert');
    await until(()=>js(`return location.hash==='#expert' && !!document.querySelector('section a[href="#expert-mmdvmhost"]')`));
    const choices=await js(`return [...document.querySelectorAll('section li a')].map(a=>a.innerText.trim())`);
    console.log('Expert → Quick Edit:',choices);
    assert.deepEqual(choices,['MMDVMHost · DMR','DMRGateway · сеть 1']);
    await follow('section a[href="#expert-mmdvmhost"]');
    assert.deepEqual((await fields('#expert-mmdvmhost','fieldset:not(:disabled) input[name=cc]')).fields,host);
    await follow('nav a[href="#expert"]');
    await follow('section a[href="#expert-dmrgateway"]');
    const gw=await fields('#expert-dmrgateway','fieldset:not(:disabled) input[name=rule0]');
    assert.deepEqual(gw.fields.slice(0,3),['Включить сеть 1','Мастер-сервер','Порт']);
    assert(gw.fields.includes('Правило TG 1'));
    console.log('PASS menu navigation on '+width+'px; no save submitted');
  }
  assert.equal(posts.length,0);
  console.log('PASS desktop and mobile; pinned HTTPS, GET/HEAD only. Configuration has DMR flags, no multi-protocol selector.');
  assert.equal(stationErrors.length,0,JSON.stringify(stationErrors));
}
