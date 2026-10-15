<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { api, ApiError, apiWithKeelGate, keelGate } from '@nrynss/chaaya/keel';
  import { Dialog } from 'bits-ui';
  import Keys from '../keys/Keys.svelte';
  import type { Notice } from '../errors/notice';
  import { refusal } from './messages';

  type Request = { title: string; age: number };
  type Card = { id: string; title: string; age: number; verdict: keyof typeof verdicts };
  const verdicts = { yes: 'Yes', yes_with_settings: 'Yes, with settings', no: 'No', unclear: 'Not enough evidence' };
  const gate = keelGate();
  const call = apiWithKeelGate(gate);
  let title = $state('');
  let age = $state<number | undefined>();
  let pending = $state(false);
  let notice = $state<Notice | undefined>();
  let cards = $state<Card[]>([]);
  let list = $state<'loading' | 'ready' | 'failed'>('loading');
  let open = $state(false);
  let keysOpen = $state(false);
  let passcode = $state('');
  let passcodeError = $state('');
  let saved = $state<Request | undefined>();
  // Set while closing the passcode dialog to offer keys, so that close does not drop the question or the passcode.
  let keepQuestion = false;
  let submitButton: HTMLButtonElement;
  let passcodeInput: HTMLInputElement;
  const controller = new AbortController();
  onDestroy(() => { controller.abort(); gate.clear(); });
  function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
  function card(value: unknown): value is Card {
    return record(value) && typeof value.id === 'string' && /^c_[A-Za-z0-9_-]+$/.test(value.id) && typeof value.title === 'string' && value.title.trim().length > 0 && Number.isInteger(value.age) && Number(value.age) >= 3 && Number(value.age) <= 17 && typeof value.verdict === 'string' && Object.hasOwn(verdicts, value.verdict);
  }
  onMount(async () => {
    try {
      const response = await api<unknown>('/api/cards', { signal: controller.signal, timeoutMs: 15000 });
      if (!record(response) || !Array.isArray(response.cards) || !response.cards.every(card) || new Set(response.cards.map(item => item.id)).size !== response.cards.length) throw new Error('Invalid cards');
      cards = response.cards;
      list = 'ready';
    } catch { if (!controller.signal.aborted) list = 'failed'; }
  });
  async function start(event: SubmitEvent) {
    event.preventDefault();
    if (pending || open || keysOpen) return;
    if (!title.trim() || !Number.isInteger(age) || Number(age) < 3 || Number(age) > 17) {
      notice = { situation: 'The question needs a title and an age.', next: 'Enter a title and a whole age from 3 to 17.' };
      return;
    }
    saved = { title: title.trim(), age: Number(age) };
    notice = undefined;
    await send();
  }
  async function send() {
    if (!saved || pending) return;
    pending = true;
    try {
      const response = await call<unknown>('/api/checks', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(saved), signal: controller.signal, timeoutMs: 30000 });
      if (!record(response)) throw new Error('Invalid answer');
      if (typeof response.card_id === 'string' && /^c_[A-Za-z0-9_-]+$/.test(response.card_id) && !('check_id' in response)) {
        await goto(resolve('/card/[id]', { id: response.card_id }));
      } else if (typeof response.check_id === 'string' && /^j_[A-Za-z0-9_-]+$/.test(response.check_id) && !('card_id' in response)) {
        await goto(resolve('/check/[id]', { id: response.check_id }), { state: { checkTitle: saved.title } });
      } else throw new Error('Invalid answer');
    } catch (error) {
      if (controller.signal.aborted) return;
      if (error instanceof ApiError && error.code === 'passcode_required') {
        keysOpen = false;
        if (open) passcodeError = 'That passcode wasn’t accepted. Try again.';
        gate.clear(); passcode = ''; notice = undefined; open = true;
      } else {
        notice = refusal(error);
        const offerKeys = error instanceof ApiError && error.code === 'budget_exhausted';
        if (offerKeys) {
          keepQuestion = true;
          passcode = ''; passcodeError = '';
          open = false;
          keepQuestion = false;
          keysOpen = true;
        } else { open = false; keysOpen = false; saved = undefined; }
      }
    } finally {
      pending = false;
      if (open) { await tick(); passcodeInput?.focus(); }
    }
  }
  async function unlock(event: SubmitEvent) {
    event.preventDefault();
    if (pending || !passcode.trim()) return;
    try { gate.set(passcode); } catch { passcodeError = 'Enter a passcode without line breaks.'; return; }
    passcode = ''; passcodeError = '';
    await send();
  }
  function close(next: boolean) {
    open = next;
    if (next || keepQuestion) return;
    passcode = ''; passcodeError = ''; gate.clear(); saved = undefined;
  }
</script>

<section class="panel ask-panel" aria-labelledby="ask-title">
  <h2 id="ask-title">What are they asking for?</h2>
  <form onsubmit={start} aria-busy={pending}>
    <div class="fields">
      <label class="title-field" for="asked-title">App, game or channel<input id="asked-title" name="title" bind:value={title} required disabled={pending || open || keysOpen} placeholder="What your child asked for" autocomplete="off" /></label>
      <label for="child-age">Child’s age<input id="child-age" name="age" type="number" bind:value={age} min="3" max="17" step="1" required disabled={pending || open || keysOpen} placeholder="3–17" inputmode="numeric" /></label>
    </div>
    <button class="primary" type="submit" disabled={pending || open || keysOpen} bind:this={submitButton}>{pending ? 'Starting your check…' : 'Check this title'}</button>
    {#if notice}<section class="notice" role="alert"><p>{notice.situation}</p><p>{notice.next}</p></section>{/if}
  </form>
</section>
<section class="examples" id="examples" aria-labelledby="examples-title" aria-busy={list === 'loading'}>
  <div class="examples-heading"><h2 id="examples-title">Previous checks</h2></div>
  {#if list === 'loading'}<p role="status">Loading previous checks…</p>
  {:else if list === 'failed'}<p>Previous checks couldn’t be loaded. Refresh to try again, or enter a title above.</p>
  {:else if !cards.length}<p>No checks yet. Enter a title above to get started.</p>
  {:else}<ul>{#each cards as item (item.id)}<li><a href={resolve('/card/[id]', { id: item.id })}><span class="example-title">{item.title}<span class="example-age">For age {item.age}</span></span><span class="verdict" class:yes={item.verdict === 'yes'} class:no={item.verdict === 'no'} class:caution={item.verdict === 'yes_with_settings' || item.verdict === 'unclear'}>{verdicts[item.verdict]}</span></a></li>{/each}</ul>{/if}
</section>
<Dialog.Root bind:open onOpenChange={close}>
  <Dialog.Portal><Dialog.Overlay class="ask-overlay" />
    <Dialog.Content class="ask-dialog" onCloseAutoFocus={(event) => { event.preventDefault(); submitButton?.focus(); }} onEscapeKeydown={(event) => { if (pending) event.preventDefault(); }} onInteractOutside={(event) => { if (pending) event.preventDefault(); }}>
      <Dialog.Title level={2}>Enter the passcode</Dialog.Title>
      <Dialog.Description>This demo requires a passcode to start a check.</Dialog.Description>
      <form onsubmit={unlock} aria-busy={pending}>
        <label for="shared-passcode">Passcode<input id="shared-passcode" type="password" bind:value={passcode} bind:this={passcodeInput} required disabled={pending} autocomplete="off" aria-invalid={!!passcodeError} aria-describedby={passcodeError ? 'passcode-error' : undefined} /></label>
        {#if passcodeError}<p id="passcode-error" class="notice" role="alert">{passcodeError}</p>{/if}
        <button type="submit" class="primary" disabled={pending}>{pending ? 'Trying your passcode…' : 'Continue the check'}</button>
        <Dialog.Close class="cancel" disabled={pending}>Cancel</Dialog.Close>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
{#if keysOpen && saved}<Keys title={saved.title} age={saved.age} api={call} bind:open={keysOpen} />{/if}

<style>
  .ask-panel { border-top: 3px solid var(--accent); }
  .fields { display: grid; grid-template-columns: minmax(0, 1fr) 100px; gap: 18px; }
  label { display: block; font-size: .82rem; font-weight: 650; color: var(--text); }
  input { display: block; width: 100%; min-height: 52px; margin-top: 9px; padding: 12px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface); color: var(--text); font: inherit; font-size: 1rem; font-weight: 400; }
  input::placeholder { color: var(--faint); opacity: 1; }
  button { min-height: 52px; font: inherit; font-size: .94rem; font-weight: 650; cursor: pointer; border-radius: var(--radius-control); }
  button:disabled, input:disabled { cursor: wait; }
  .primary { width: 100%; border: 1px solid var(--accent); background: var(--accent); color: var(--on-accent); margin-top: 22px; padding: 12px 18px; }
  .primary:hover { box-shadow: 0 0 0 2px var(--accent-soft); }
  .notice { background: var(--warn-soft); color: var(--warn); border-left: 3px solid var(--warn); padding: 13px 15px; font-size: .9rem; margin: 18px 0 0; }
  .notice p { margin: 0; overflow-wrap: anywhere; }
  .notice p + p { margin-top: 8px; }
  .examples { margin-top: 36px; }
  .examples-heading { display: flex; justify-content: space-between; align-items: baseline; gap: 16px; margin-bottom: 12px; }
  .examples h2 { font-size: 1.35rem; margin: 0; }
  .examples > p { font-size: .85rem; }
  ul { margin: 0; padding: 0; list-style: none; }
  li { border-top: 1px solid var(--line-soft); }
  li a { display: flex; justify-content: space-between; align-items: center; gap: 20px; min-height: 85px; padding: 15px 0; text-decoration: none; color: var(--text); }
  li a:hover .example-title { text-decoration: underline; text-underline-offset: .22em; }
  .example-title { font-size: .95rem; font-weight: 600; }
  .example-age { display: block; color: var(--dim); font-size: .75rem; font-weight: 400; margin-top: 3px; }
  .verdict { font-size: .72rem; text-align: right; padding: 4px 8px; border-radius: 4px; max-width: 145px; }
  .yes { color: var(--ok); background: var(--ok-soft); }.no { color: var(--stop); background: var(--stop-soft); }.caution { color: var(--warn); background: var(--warn-soft); }
  :global(.ask-overlay) { position: fixed; inset: 0; z-index: 20; background: #0009; }
  :global(.ask-dialog) { position: fixed; z-index: 21; left: 50%; top: 50%; transform: translate(-50%, -50%); width: min(440px, calc(100% - 32px)); max-height: calc(100dvh - 32px); overflow-y: auto; background: var(--surface); color: var(--text); border: 1px solid var(--line); border-radius: var(--radius-panel); padding: 28px; }
  :global(.ask-dialog [data-dialog-title]) { font-family: Georgia, Cambria, serif; font-size: 1.8rem; font-weight: 400; margin-bottom: 16px; }
  :global(.ask-dialog [data-dialog-description]) { color: var(--dim); font-size: .9rem; margin-bottom: 22px; }
  :global(.ask-dialog .cancel) { width: 100%; min-height: 52px; font: inherit; font-size: .94rem; font-weight: 650; cursor: pointer; border-radius: var(--radius-control); margin-top: 10px; border: 1px solid var(--line); background: var(--surface); color: var(--text); }
  @media (max-width: 480px) { .ask-panel { padding: 20px; } .ask-panel h2 { font-size: 1.5rem; } .fields { grid-template-columns: minmax(0, 1fr); gap: 15px; } #child-age { max-width: 130px; } .examples-heading { display: block; } }
</style>
