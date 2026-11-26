<script lang="ts">
  import { onMount } from 'svelte';
  type Modes = Record<string, Record<string,string>>;
  const endpoint='/manage/api/protocols';
  const order=['DMR','D-Star','M17','System Fusion','P25','NXDN','POCSAG','FM'];
  const labels:Record<string,string>={ColorCode:'Color Code (0–15)',Module:'Модуль D-Star',Frequency:'Частота, Гц',
    TXHang:'Задержка после передачи (TXHang), с',LowDeviation:'Узкая девиация',SelfOnly:'Только свой позывной',
    RemoteGateway:'Удалённый шлюз',ModeHang:'Удержание режима, с',Beacons:'Маяки DMR',CallHang:'Удержание вызова, с',
    'net.ModeHang':'Удержание сети, с','net.Debug':'Отладка сети',
    'net.Enable':'Включить сеть','net.GatewayAddress':'Адрес шлюза','net.GatewayPort':'Порт шлюза',
    'net.LocalAddress':'Локальный адрес','net.LocalPort':'Локальный порт','net.Slot1':'Таймслот 1','net.Slot2':'Таймслот 2'};
  const flags=new Set(['net.Enable','net.Debug','net.Slot1','net.Slot2','SelfOnly','AckReply','ErrorReply','RemoteGateway',
    'LowDeviation','Beacons','EmbeddedLCOnly','DumpTAData','OverrideUIDCheck','SoftId']);
  let original:Modes={}, csrf='', revision='';
  let draft=$state<Modes>({}),busy=$state(true),error=$state(''),message=$state('');
  let current=$state('');
  const changes=$derived.by(()=>{
    const diff:Modes={};
    for(const [mode,fields] of Object.entries(draft)){
      const delta=Object.fromEntries(Object.entries(fields).filter(([k,v])=>v!==original[mode][k]&&
        (fields.Enable==='1'||k==='Enable'||k==='net.Enable')));
      if(Object.keys(delta).length)diff[mode]=delta;
    }
    return diff;
  });
  const dirty=$derived(Object.keys(changes).length>0);
  const title=(mode:string)=>mode==='System Fusion'?'YSF':mode;
  function show(mode:string){
    current=mode;
    try{sessionStorage.setItem('pistar.radio.editor',mode)}catch{/* optional UI preference */}
  }
  async function load(){
    const r=await fetch(endpoint,{cache:'no-store'});
    if(!r.ok)throw Error(`Не удалось прочитать MMDVMHost (HTTP ${r.status}).`);
    const body=await r.json(); original=body.config.modes; draft=structuredClone(original); csrf=body.csrf; revision=body.config.revision;
    if(draft[current]?.Enable!=='1')show(order.find(m=>draft[m]?.Enable==='1')??'');
  }
  function toggle(mode:string,on:boolean){
    const fields=draft[mode];
    fields.Enable=on?'1':'0';
    if('net.Enable' in fields)fields['net.Enable']=fields.Enable;
    if(on)show(mode);
    else if(current===mode)show(order.find(m=>draft[m]?.Enable==='1')??'');
    message='';
  }
  async function run(save:boolean){
    busy=true; error=''; message='';
    try{
      if(save){
        const r=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/json','X-PiStar-CSRF':csrf},body:JSON.stringify({revision,changes})});
        if(!r.ok)throw Error((await r.text()).trim() || `HTTP ${r.status}`);
      }
      await load();
      if(save)message='Настройки сохранены.';
    }catch(e){error=e instanceof Error?e.message:'Ошибка связи';}
    finally{busy=false;}
  }
  onMount(()=>{try{current=sessionStorage.getItem('pistar.radio.editor')??''}catch{}void run(false)});
</script>
<section class="radio" aria-label="Протоколы MMDVMHost">
  <header><div><p class="kicker">Configuration / MMDVMHost</p><h1>Протоколы радиостанции</h1></div><a href="#settings">Общие настройки станции →</a></header>
  {#if Object.keys(draft).length}
    <form onsubmit={e=>{e.preventDefault();void run(true)}}>
      <div class="panels">
      <fieldset class="modes" disabled={busy}><legend>Используемые протоколы</legend>
        <p class="hint">Включите режим, чтобы настроить его.</p>
        {#each order.filter(m=>draft[m]) as mode}
          <div class="row" class:chosen={current===mode}>
            <label><input type="checkbox" name={'mode-'+mode} checked={draft[mode].Enable==='1'}
              onchange={e=>toggle(mode,e.currentTarget.checked)} /> {title(mode)}</label>
            {#if draft[mode].Enable==='1'}
              <button class="inspect" type="button" aria-label={'Настроить '+title(mode)} aria-pressed={current===mode} onclick={()=>show(mode)}>{current===mode?'Открыт':'Настроить'}</button>
            {/if}
          </div>
        {/each}
      </fieldset>
      {#if current && draft[current]?.Enable==='1'}
        {@const mode=current}
        <fieldset class="editor" disabled={busy} data-protocol={mode}><legend>Параметры {title(mode)}</legend>
        {#each [false,true] as network}
          {@const keys=Object.keys(draft[mode]).filter(k=>k!=='Enable'&&k.startsWith('net.')===network)}
          {#if keys.length}
          <h2>{network?'Сетевое подключение':'Радиопараметры'}</h2><div class="grid">
          {#each keys as key}
            <label>{labels[key]??key}
              {#if flags.has(key)}
                <input type="checkbox" name={mode+'.'+key} checked={draft[mode][key]==='1'} onchange={e=>draft[mode][key]=e.currentTarget.checked?'1':'0'} />
              {:else}
                <input name={mode==='DMR'&&key==='ColorCode'?'cc':mode+'.'+key} bind:value={draft[mode][key]} required />
              {/if}
            </label>
          {/each}
          </div>{/if}
        {/each}
          {#if mode==='DMR'}<a href="#expert-dmrgateway">Настроить DMRGateway и правила TG →</a>{/if}
        </fieldset>
      {:else}<div class="empty">Выберите протокол слева или сверху. Его параметры появятся здесь.</div>{/if}
      </div>
      <div class="bar"><div><strong>{dirty?'Есть несохранённые изменения':'Настройки с узла'}</strong><p>При сохранении изменений MMDVMHost перезапустится.</p></div>
        <button type="submit" disabled={busy||!dirty}>Сохранить изменения</button></div>
    </form>
  {:else if !busy && !error}<p>В INI узла не найдены секции поддерживаемых протоколов.</p>{/if}
  <button class="reload" type="button" disabled={busy} onclick={()=>void run(false)}>Перечитать с узла</button>
  {#if error}<p role="alert">{error}</p>{/if}
  {#if message}<p role="status">{message}</p>{/if}
</section>
<style>
  .radio{color:#26242b}.radio header,.bar,.row{display:flex;align-items:center;justify-content:space-between;gap:12px}
  header{margin:28px 0 18px;flex-wrap:wrap}h1{font-size:clamp(1.5rem,3vw,2rem);margin:0;letter-spacing:-.035em}.kicker,h2{font-size:.8rem;color:#63455a;margin:0 0 4px;font-weight:700}
  .panels{display:grid;grid-template-columns:minmax(200px,1fr) minmax(0,2.4fr);align-items:start;gap:16px}
  fieldset,.empty,.bar{box-sizing:border-box;min-width:0;margin:0;padding:16px;border:1px solid #d9d4da;border-radius:10px;background:white}
  legend{padding:0 5px;font-size:1.05rem;font-weight:700}p{margin:0}.hint,.bar p,.empty{font-size:.84rem;color:#58515b}
  .row{border-bottom:1px solid #eae6ea;min-height:42px}.row:last-child{border:0}.chosen{color:#71133e}
  .row label{display:flex;align-items:center;gap:9px;font-weight:650;cursor:pointer;flex:1;min-height:42px}
  input[type=checkbox]{accent-color:#81244f;width:18px;height:18px}
  .editor h2{margin:8px 0 10px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,175px),1fr));gap:10px 16px;margin-bottom:16px}
  .grid label{display:flex;flex-direction:column;gap:4px;font-size:.88rem}.grid label:has(input[type=checkbox]){flex-direction:row-reverse;justify-content:flex-end;align-items:center}
  input:not([type=checkbox]){box-sizing:border-box;width:100%;font:inherit;border:1px solid #b7aeba;border-radius:5px;padding:8px}
  button{font:inherit;cursor:pointer;border-radius:6px}.inspect,.reload{border:0;background:transparent;color:#76224c;text-decoration:underline;min-height:40px}.inspect{font-size:.8rem}.inspect[aria-pressed=true]{color:#57525a;text-decoration:none}
  .bar{margin-top:14px;background:#fcfafc}.bar button{border:0;background:#75214b;color:white;padding:10px 16px;font-weight:650}.bar button:disabled{background:#e2dce2;color:#635a65;cursor:default}
  [role=status]{color:#116044}[role=alert]{color:#96213d}a{color:#75214b}
  @media(max-width:640px){.panels{grid-template-columns:1fr;gap:10px}.bar{align-items:stretch;flex-direction:column}.modes,.editor{padding:12px}header{margin-top:18px}}
</style>
