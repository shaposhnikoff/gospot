import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';

export async function checkPackaged({write,wd,js,click,until,ssh,root,admin,posts}) {
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
  const meta=()=>ssh(`stat -c '%U:%G %a' ${paths.join(' ')}`);
  const ids=()=>['mmdvmhost','dmrgateway'].map(u=>ssh(`systemctl show -p InvocationID --value ${u}`));
  const expected=write?read():null, metadata=meta();
  const hash=s=>createHash('sha256').update(Buffer.from(s,'latin1')).digest('hex');
  for(const [i,id,field,old,next,from,to] of [
    [0,'configuration','cc','1','12','ColorCode=1','ColorCode=12'],
    [1,'expert','rule0','2,91,2,91,1','2,91,2,310,1','TGRewrite0=2,91,2,91,1','TGRewrite0=2,91,2,310,1']
  ]){
    const sel=`input[name="${field}"]`;
    async function open(){
      await menu(id);
      await until(()=>js('return !!document.querySelector(arguments[0])?.getClientRects().length',sel));
      assert.equal(await js('return location.href'),admin+'/manage/#'+(i?'expert-dmrgateway':'settings'));
      console.log(`Menu → ${id}${i?' → DMRGateway':''}: ${field} visible`);
    }
    await open();
    if(!write)continue;
    const value=()=>js('return document.querySelector(arguments[0]).value',sel);
    assert.equal(await value(),old);
    const initial=ids();
    const el=await wd('/element',{using:'css selector',value:sel});
    const path='/element/'+el['element-6066-11e4-a52e-4f735466cecf'];
    await wd(path+'/value',{text:'\ue009a\ue000'+next});
    await click(`form:has(${sel}) button`);
    await until(()=>js(`return [...document.querySelectorAll('[role=status]')].some(n=>n.innerText.includes('Настройки сохранены'))`));
    assert.equal(posts.length,i+1);
    assert.equal(posts[i].status,204);
    assert(posts[i].csrf);
    assert.equal(posts[i].body[field==='cc'?'cc':'rules'] instanceof Array ? posts[i].body.rules[0] : String(posts[i].body.cc),next);
    await open();
    assert.equal(await value(),next);
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
}
