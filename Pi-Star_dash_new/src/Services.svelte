<script lang="ts">
  import { onMount } from 'svelte';
  type Config = {dmr:number; network:number; cc:number; ts1:number; ts2:number; gateway:number; master:string; port:number; rules:string[]; revision:string};
  let {editor='both'}:{editor?:'both'|'host'|'gateway'}=$props();
  const endpoint='/manage/api/services';
  const flags=[['dmr','Режим DMR'],['network','Сеть DMR'],['ts1','Таймслот 1'],['ts2','Таймслот 2']] as const;
  let cfg=$state<Config|null>(null), csrf='', busy=$state(false), message=$state(''), error=$state('');
  async function load() {
    const r=await fetch(endpoint,{cache:'no-store'});
    if(!r.ok) throw Error(`Не удалось прочитать настройки (HTTP ${r.status}).`);
    const body=await r.json(); cfg=body.config; csrf=body.csrf;
  }
  async function run(save:boolean) {
    if(busy) return;
    busy=true; message=''; error='';
    try {
      if(save) {
        const r=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/json','X-GoSpot-CSRF':csrf},body:JSON.stringify(cfg)});
        if(!r.ok) throw Error((await r.text()).trim() || `HTTP ${r.status}`);
      }
       await load();
       if(save)message='Настройки сохранены.';
    } catch(e) {error=e instanceof Error?e.message:'Ошибка связи с узлом.';}
    finally {busy=false;}
  }
  onMount(()=>{void run(false)});
</script>
<section>
  <h2>{editor==='host'?'DMR Configuration':editor==='gateway'?'Expert · DMRGateway':'Сервисы · DMR'}</h2>
  <p>Изменения применяются к службам этого узла. При сохранении изменённая служба перезапустится.</p>
  {#if cfg}
    <form onsubmit={e=>{e.preventDefault();void run(true)}}>
      {#if editor!=='gateway'}<fieldset disabled={busy}><legend>MMDVMHost</legend>
        {#each flags as [key,label]}
          <label><input type="checkbox" checked={cfg[key]===1} onchange={e=>{if(cfg)cfg[key]=Number(e.currentTarget.checked)}} /> {label}</label>
        {/each}
        <label>Color Code (0–15) <input required name="cc" type="number" min="0" max="15" bind:value={cfg.cc} /></label>
      </fieldset>{/if}
      {#if editor!=='host'}<fieldset disabled={busy}><legend>DMRGateway · сеть 1</legend>
        <label><input type="checkbox" checked={cfg.gateway===1} onchange={e=>{if(cfg)cfg.gateway=Number(e.currentTarget.checked)}} /> Включить сеть 1</label>
        <label>Мастер-сервер <input required bind:value={cfg.master} /></label>
        <label>Порт <input required type="number" min="1" max="65535" bind:value={cfg.port} /></label>
        {#if cfg.rules.length}<p>Правило TG: входной слот, исходная TG, выходной слот, целевая TG, количество групп.</p>{/if}
        {#each cfg.rules as _,i}
          <label>Правило TG {i+1} <input required name={'rule'+i} bind:value={cfg.rules[i]} /></label>
        {/each}
      </fieldset>{/if}
      <button disabled={busy}>Сохранить настройки DMR</button>
    </form>
  {/if}
  <button type="button" disabled={busy} onclick={()=>void run(false)}>Перечитать с узла</button>
  <a href="#settings">Настройки станции и откат последнего изменения</a>
  {#if error}<p role="alert">{error}</p>{/if}
  {#if message}<p role="status">{message}</p>{/if}
</section>
<style>
  fieldset{border:1px solid #d6cdd2;border-radius:8px;margin:1rem 0;padding:1rem;min-width:0}legend{font-weight:700}
  label{display:block;margin:.4rem 0}input:not([type=checkbox]){display:block;padding:.4rem;max-width:95%}
  button{padding:.5rem .85rem;margin:.35rem .5rem .35rem 0}a{color:#722946}[role=alert]{color:#96213d}
</style>
