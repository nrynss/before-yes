<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { api, ApiError, keelErrorParser, type ApiRequestInit } from '@nrynss/chaaya/keel';
  import { Dialog } from 'bits-ui';

  // Mount when the daily cap is spent. The parent supplies the title, the age,
  // and optionally the passcode-gated api. Keys stay in the two fields below:
  // never in storage, a cookie, the URL, or a log.
  type CheckApi = <T>(path: string, init?: ApiRequestInit) => Promise<T>;
  let {
    title,
    age,
    api: call = api,
    open = $bindable(true),
  }: {
    title: string;
    age: number;
    api?: CheckApi;
    open?: boolean;
  } = $props();

  let tokenKey = $state('');
  let tavilyKey = $state('');
  let pending = $state(false);
  let message = $state('');
  let tokenInput: HTMLInputElement | undefined;
  let controller = new AbortController();

  const cardId = /^c_[0-9a-f]{32}$/;
  const checkId = /^j_[0-9a-f]{32}$/;

  onMount(() => { void tick().then(() => { if (open) tokenInput?.focus(); }); });
  onDestroy(() => { controller.abort(); });

  function stop() {
    controller.abort();
    controller = new AbortController();
  }
  function clearKeys() {
    tokenKey = '';
    tavilyKey = '';
  }
  function close(next: boolean) {
    open = next;
    if (next) return;
    clearKeys();
    message = '';
    pending = false;
    stop();
  }
  function record(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
  }
  // Same rule as the server: printable ASCII, no spaces, at most 512 bytes.
  function usableKey(key: string): boolean {
    if (key.length === 0 || key.length > 512) return false;
    for (let i = 0; i < key.length; i += 1) {
      const code = key.charCodeAt(i);
      if (code <= 0x20 || code > 0x7e) return false;
    }
    return true;
  }
  // Branch on the code only. The server message is never shown: it might quote a key.
  function refusal(error: unknown): string {
    const code = error instanceof ApiError ? error.code : '';
    switch (code) {
      case 'invalid_request': return 'Those keys were refused. Enter both a Token Factory key and a Tavily key, with no spaces or unusual characters. Nothing was stored.';
      case 'budget_exhausted': return 'The budget for fresh checks is used up. These keys were not stored.';
      case 'live_checks_off': return 'Fresh checks are paused. These keys were not stored.';
      case 'busy': return 'Other checks are running. Wait a moment, then try again. These keys were not stored.';
      case 'rate_limited': return 'There have been several requests. Wait a moment, then try again. These keys were not stored.';
      case 'passcode_required': return 'This check needs the shared passcode. These keys were not stored.';
      case 'network':
      case 'timeout': return 'We couldn’t reach Before Yes. These keys were not stored. Try again.';
      default: return 'Before Yes couldn’t start this check. These keys were not stored. Try again.';
    }
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (pending || !open) return;
    const tokenFactory = tokenKey.trim();
    const tavily = tavilyKey.trim();
    // A half pair is not a request. Both headers go out, or neither does.
    if (tokenFactory === '' || tavily === '') {
      message = 'Enter both a Token Factory key and a Tavily key. One on its own is not sent.';
      return;
    }
    if (!usableKey(tokenFactory) || !usableKey(tavily)) {
      message = 'A key can’t contain spaces or unusual characters, and it must be 512 bytes or shorter. Nothing was sent.';
      return;
    }
    message = '';
    pending = true;
    const signal = controller.signal;
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      'X-Token-Factory-Key': tokenFactory,
      'X-Tavily-Key': tavily,
    };
    try {
      const response = await call<unknown>('/api/checks', {
        method: 'POST',
        headers,
        body: JSON.stringify({ title, age }),
        signal,
        timeoutMs: 30000,
        parseError: keelErrorParser,
      });
      if (signal.aborted || !open) return;
      if (record(response) && typeof response.card_id === 'string' && cardId.test(response.card_id) && !('check_id' in response)) {
        const id = response.card_id;
        clearKeys();
        await goto(resolve('/card/[id]', { id }));
        return;
      }
      if (record(response) && typeof response.check_id === 'string' && checkId.test(response.check_id) && !('card_id' in response)) {
        const id = response.check_id;
        clearKeys();
        await goto(resolve('/check/[id]', { id }));
        return;
      }
      message = 'The answer didn’t name a check or a card we can open. The keys were not stored.';
    } catch (error) {
      if (signal.aborted || !open) return;
      message = refusal(error);
    } finally {
      if (!signal.aborted) pending = false;
    }
  }
</script>

<Dialog.Root bind:open onOpenChange={close}>
  <Dialog.Portal>
    <Dialog.Overlay class="keys-overlay" />
    <Dialog.Content class="keys-dialog" onOpenAutoFocus={(event) => { event.preventDefault(); tokenInput?.focus(); }} onEscapeKeydown={(event) => { if (pending) event.preventDefault(); }} onInteractOutside={(event) => { if (pending) event.preventDefault(); }}>
      <Dialog.Title level={2}>Use your own keys for this check.</Dialog.Title>
      <Dialog.Description>Today’s free checks are used up. Paste a Token Factory key and a Tavily key to run this one check on your own accounts. The keys are used for this one check and not stored.</Dialog.Description>
      <p class="subject">For {title}, age {age}.</p>
      <form method="post" autocomplete="off" novalidate onsubmit={submit} aria-busy={pending}>
        <label for="token-factory-key">Token Factory key<input id="token-factory-key" type="text" bind:value={tokenKey} bind:this={tokenInput} autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" disabled={pending} /></label>
        <label for="tavily-key">Tavily key<input id="tavily-key" type="text" bind:value={tavilyKey} autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" disabled={pending} /></label>
        {#if message}<p id="keys-error" class="notice" role="alert">{message}</p>{/if}
        <button class="primary" type="submit" disabled={pending}>{pending ? 'Starting your check…' : 'Run this check'}</button>
        <Dialog.Close class="cancel" disabled={pending}>Cancel</Dialog.Close>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  :global(.keys-overlay) { position: fixed; inset: 0; z-index: 20; background: #0009; }
  :global(.keys-dialog) { position: fixed; z-index: 21; left: 50%; top: 50%; transform: translate(-50%, -50%); width: min(440px, calc(100% - 32px)); max-height: calc(100dvh - 32px); overflow-y: auto; background: var(--surface, #fff); color: var(--text, #172c25); border: 1px solid var(--line, #b6baac); border-radius: var(--radius-panel, 14px); padding: 28px; }
  :global(.keys-dialog [data-dialog-title]) { font-family: Georgia, Cambria, serif; font-size: 1.8rem; font-weight: 400; line-height: 1.25; margin: 0 0 16px; }
  :global(.keys-dialog [data-dialog-description]) { color: var(--dim, #4f6057); font-size: .9rem; margin: 0 0 18px; }
  :global(.keys-dialog .subject) { color: var(--text, #172c25); font-size: .95rem; font-weight: 650; margin: 0 0 18px; overflow-wrap: anywhere; }
  :global(.keys-dialog label) { display: block; font-size: .82rem; font-weight: 650; color: var(--text, #172c25); }
  :global(.keys-dialog input) { display: block; width: 100%; min-height: 52px; margin: 9px 0 16px; padding: 12px; border: 1px solid var(--line, #b6baac); border-radius: var(--radius-control, 8px); background: var(--surface, #fff); color: var(--text, #172c25); font: inherit; font-size: 1rem; font-weight: 400; }
  :global(.keys-dialog button) { min-height: 52px; font: inherit; font-size: .94rem; font-weight: 650; cursor: pointer; border-radius: var(--radius-control, 8px); }
  :global(.keys-dialog button:disabled), :global(.keys-dialog input:disabled) { cursor: wait; }
  :global(.keys-dialog .primary) { width: 100%; border: 1px solid var(--accent, #245c45); background: var(--accent, #245c45); color: var(--on-accent, #fff); padding: 12px 18px; }
  :global(.keys-dialog .cancel) { width: 100%; margin-top: 10px; border: 1px solid var(--line, #b6baac); background: var(--surface, #fff); color: var(--text, #172c25); }
  :global(.keys-dialog .notice) { background: var(--warn-soft, #f4e9ce); color: var(--warn, #82590f); border-left: 3px solid var(--warn, #82590f); padding: 13px 15px; font-size: .9rem; margin: 0 0 16px; overflow-wrap: anywhere; }
  @media (max-width: 480px) { :global(.keys-dialog) { padding: 20px; } :global(.keys-dialog [data-dialog-title]) { font-size: 1.5rem; } }
</style>
