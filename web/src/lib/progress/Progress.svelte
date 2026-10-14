<script lang="ts">
 import { onMount } from 'svelte';
 import { goto } from '$app/navigation';
 import { resolve } from '$app/paths';
 import { api, JobStream, type JobSnapshot } from '@nrynss/chaaya/keel';
 import { failure, snapshot, validId } from './snapshot';
 let {id}: {id:string} = $props();
 let stream = $state<JobStream>();
 let problem = $state('');
 let initial = $state('Loading the check…');
 let handled = false;
 let alive = false;
 let controller: AbortController;
 const stages = [{id:'gather',label:'Gather',text:'Find policies, reviews and parent reports.'},{id:'read',label:'Read',text:'Read the sources and keep the evidence.'},{id:'judge',label:'Judge',text:'Consider the evidence against the same checklist.'}];
 const latest = $derived(stream?.events.findLast(event => event.name === 'progress'));
 const stage = $derived(stream?.stage);
 const detail = $derived(latest?.name === 'progress' && typeof latest.detail === 'object' && latest.detail !== null ? latest.detail as Record<string, unknown> : {});
 const message = $derived(typeof detail.message === 'string' ? detail.message : '');
 const current = $derived(latest?.name === 'progress' ? latest.current : undefined);
 const total = $derived(latest?.name === 'progress' ? latest.total : undefined);
 const active = $derived(stage === 'alternatives' ? 3 : stages.findIndex(item => item.id === stage));
 async function read() {return snapshot(await api<unknown>(`/api/checks/${id}`,{signal:controller.signal,timeoutMs:15000}),id);}
 async function finish(state: JobSnapshot & {cardId?:string}) {
  if (!alive) return;
  if (state.status === 'done' && state.cardId) await goto(resolve('/card/[id]',{id:state.cardId}));
  else if (state.status === 'error') problem = failure(state.error);
  else if (state.status === 'cancelled') problem = 'This check was cancelled. Return to the ask page to start it again.';
  else if (state.status === 'interrupted') problem = 'A restart interrupted this check. Return to the ask page to start it again.';
 }
 async function start() {
  stream?.close(); controller?.abort(); controller = new AbortController();
  problem = ''; handled = false; stream = undefined;
  if (!validId(id,'j')) {problem = failure({code:'not_found'});return;}
  try {
   const state = await read();
   if (!alive) return;
   if (!['queued','running'].includes(state.status)) {await finish(state);return;}
   initial = state.status === 'queued' ? 'Your check is queued.' : 'The research is running.';
   stream = new JobStream({url:`/api/checks/${id}/events`,fetchState:async () => {
    try {return await read();} catch(error) {if(alive) {problem = failure(error);stream?.close();} throw error;}
   }});
   stream.attach(task => {task();});
  } catch(error) {if(alive) problem = failure(error);}
 }
 $effect(() => {
  if(!stream || !alive || handled || !['done','error','cancelled','interrupted'].includes(stream.status)) return;
  handled = true;
  if(stream.status === 'cancelled' || stream.status === 'interrupted') void finish({jobId:id,status:stream.status});
  else void read().then(finish).catch(error => {if(alive) problem = failure(error);});
 });
 onMount(() => {alive = true;void start();return () => {alive=false;controller?.abort();stream?.close();};});
</script>
<svelte:head><title>A check, step by step — Before Yes</title></svelte:head>
<div class="reading">
 <a class="back" href={resolve('/')}>← Ask about a title</a><p class="eyebrow">The research</p>
 <h1>A check,<br />step by step.</h1><p class="intro">Good answers start with a closer look. We’ll bring you to the decision when the research is ready.</p>
 <section class="panel research" aria-label="Check progress">
  <div class="live-note"><span class="seed" aria-hidden="true"></span><span>Evidence before an answer</span></div>
  <div class="update" role="status" aria-live="polite" aria-atomic="true">
   {#if problem}<h2>Let’s take another look.</h2><p>{problem}</p>
   {:else if stream?.connection === 'failed'}<h2>The connection stopped.</h2><p>We couldn’t keep following the check. Retry to catch up with its saved state.</p>
   {:else}<h2>{message || initial}</h2>{#if current !== undefined}<p class="count">{`${current.toLocaleString()}${total !== undefined ? ` of ${total.toLocaleString()}` : ''}`} <span>read so far</span></p>{/if}
    {#if stream?.connection === 'reconnecting'}<p>Connection lost. Reconnecting to your check…</p>{/if}
    {#if stage === 'alternatives'}<p>Checking safer alternatives.</p>{/if}
   {/if}
  </div>
  {#if problem || stream?.connection === 'failed'}<button type="button" onclick={() => void start()}>Retry following this check</button>{/if}
  <ol class="steps">{#each stages as item,index (item.id)}<li class:active={index === active} class:complete={index < active} aria-current={index === active ? 'step' : undefined}><span class="step-number" aria-hidden="true">{index < active ? '✓' : `0${index+1}`}</span><div><h3>{item.label}{#if index === active}<span class="state-label">In progress</span>{:else if index < active}<span class="state-label">Complete</span>{/if}</h3><p>{item.text}</p></div></li>{/each}</ol>
 </section>
 <p class="footnote">Your check keeps running if you leave this page. No answer until the evidence has been read.</p>
</div>
<style>
 .research {padding:32px;}.live-note {display:flex;gap:10px;align-items:center;color:var(--accent);font-size:.72rem;letter-spacing:.08em;text-transform:uppercase;font-weight:650;}.seed {width:9px;height:9px;border-radius:50%;background:var(--accent);flex-shrink:0;}.update {padding:28px 0 24px;min-height:130px;}.update h2 {font-size:1.7rem;line-height:1.3;overflow-wrap:anywhere;}.update p {margin:12px 0 0;}.count {font-family:var(--font-numeric);color:var(--text);font-size:1.05rem;}.count span {font-family:var(--font-ui);font-size:.85rem;color:var(--dim);}.active .step-number {background:var(--accent);color:var(--on-accent);border-color:var(--accent);}.complete .step-number {background:var(--accent-soft);border-color:var(--accent);}.state-label {font-size:.68rem;font-weight:500;color:var(--accent);display:block;}button {font:inherit;min-height:48px;padding:10px 18px;background:var(--accent);color:var(--on-accent);border:0;border-radius:var(--radius-control);margin:0 0 24px;cursor:pointer;}.footnote {font-size:.82rem;max-width:540px;margin:22px 0 0;}@media(max-width:480px) {.research {padding:23px;}.update h2 {font-size:1.5rem;}}
</style>
