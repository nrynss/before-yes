<script lang="ts">
  import { api, ApiError } from '@nrynss/chaaya/keel';
  import { resolve } from '$app/paths';
  import { areas, verdicts, levels, parseCard, cardID, type Card, type Grade } from '$lib/card/contract';

  let { a, b } = $props<{ a: string; b: string }>();
  type Slot = { state: 'invalid' | 'unchecked' | 'loading' | 'ready' | 'missing' | 'failed'; card?: Card };
  let left = $state<Slot>({ state: 'loading' });
  let right = $state<Slot>({ state: 'loading' });
  let retry = $state(0);

  async function load(id: string, signal: AbortSignal): Promise<Slot> {
    if (!cardID(id)) return { state: 'invalid' };
    try {
      const response = await api<unknown>(`/api/cards/${encodeURIComponent(id)}`, { signal, timeoutMs: 15000 });
      return { state: 'ready', card: parseCard(response, id) };
    } catch (error) {
      if (signal.aborted) return { state: 'loading' };
      return { state: error instanceof ApiError && error.code === 'not_found' ? 'missing' : 'failed' };
    }
  }

  $effect(() => {
    const leftID = a;
    const rightID = b;
    const attempt = retry;
    void attempt;
    const controller = new AbortController();
    if (!cardID(leftID) || !cardID(rightID)) {
      left = { state: cardID(leftID) ? 'unchecked' : 'invalid' };
      right = { state: cardID(rightID) ? 'unchecked' : 'invalid' };
      return () => controller.abort();
    }
    left = { state: 'loading' };
    right = { state: 'loading' };
    void (async () => {
      const [nextLeft, nextRight] = await Promise.all([load(leftID, controller.signal), load(rightID, controller.signal)]);
      if (!controller.signal.aborted) {
        left = nextLeft;
        right = nextRight;
      }
    })();
    return () => controller.abort();
  });

  let pair = $derived(left.card && right.card ? [left.card, right.card] as const : undefined);
  const blocked = $derived(left.state === 'invalid' || right.state === 'invalid');
  const waiting = $derived(left.state === 'loading' || right.state === 'loading');
  const problem = $derived(blocked
    ? `${left.state === 'invalid' && right.state === 'invalid' ? 'These addresses are not cards' : 'One of these addresses is not a card'}. Open a card and choose Compare both, or ask about a title.`
    : left.state === 'missing' || right.state === 'missing'
      ? 'One of these cards was not found. Check the address, or open a card and choose Compare both.'
      : 'A card couldn’t be opened. Try again, or return to the ask page.');
  const sentence = (grade: Grade) => grade.reason?.trim() || (grade.level === 'unknown' ? 'Not enough verified evidence was found to answer this question.' : 'The supplied evidence supports this grade; no further explanation was provided.');
  const gradeOf = (card: Card, area: string) => card.grades.find((grade) => grade.area === area)!;
</script>
<svelte:head><title>{pair ? `${pair[0].title} and ${pair[1].title}` : 'Two titles, the same questions'} — Before Yes</title></svelte:head>
<a class="back" href={resolve('/')}>← Ask about a title</a>
<p class="eyebrow">Side by side</p>
<h1>The same questions.</h1>
<p class="intro">Two titles. One checklist.</p>
{#if waiting}
  <p role="status">Opening both cards.</p>
{:else if !pair}
  <p role="alert">{problem}</p>
  {#if !blocked}<button type="button" onclick={() => retry++}>Try again</button>{/if}
{:else}
  <div class="titles">
    <div class="spacer" aria-hidden="true"></div>
    {#each pair as card, index (card.id + ':' + index)}
      <article>
        <p class="verdict" class:stop={card.verdict === 'no'} class:yes={card.verdict === 'yes'} class:caution={card.verdict === 'unclear' || card.verdict === 'yes_with_settings'}>{verdicts[card.verdict]}</p>
        <h2>{card.title}</h2>
        <p class="meta">For age {card.age}</p>
        <a href={resolve('/card/[id]', { id: card.id })}>Open {card.title}</a>
      </article>
    {/each}
  </div>
  <div class="comparison">
    {#each areas as [areaID, label] (areaID)}
      <section class="compare-row" data-area={areaID}>
        <h2>{label}</h2>
        {#each pair as card, index (card.id + ':' + index)}
          {@const grade = gradeOf(card, areaID)}
          <div class="compare-cell" data-side={index === 0 ? 'a' : 'b'}>
            <h3>{card.title}</h3>
            <p class="grade" data-level={grade.level} class:stop={grade.level === 'flag'} class:yes={grade.level === 'ok'} class:caution={grade.level === 'ok_with_setting' || grade.level === 'unknown'}>{levels[grade.level]}</p>
            <p class="reason">{sentence(grade)}</p>
            {#if grade.setting?.trim()}<p class="setting">Setting: {grade.setting.trim()}</p>{/if}
          </div>
        {/each}
      </section>
    {/each}
  </div>
{/if}
<style>
  .titles { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); column-gap: 14px; row-gap: 4px; }
  .titles .spacer { display: none; }
  .titles article { display: grid; grid-template-rows: subgrid; grid-row: span 4; min-width: 0; }
  .titles article > * { min-width: 0; margin: 0; }
  .verdict { font-family: Georgia, Cambria, serif; font-size: clamp(1.35rem, 4vw, 2rem); line-height: 1.15; letter-spacing: -.03em; }
  .titles h2 { font-family: var(--font-ui); font-size: 1rem; font-weight: 650; letter-spacing: 0; line-height: 1.3; }
  .meta { font-size: .8rem; }
  .titles a { display: flex; align-items: center; min-height: 48px; font-size: .9rem; }
  .stop { color: var(--stop); } .yes { color: var(--ok); } .caution { color: var(--warn); }
  .grade { margin: 0 0 6px; font-size: .85rem; font-weight: 650; }
  .reason { margin: 0; font-size: .85rem; color: var(--dim); }
  .setting { margin: 8px 0 0; font-size: .85rem; color: var(--text); }
  button { min-height: 48px; margin-top: 16px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface); color: var(--text); padding: 10px 16px; font: inherit; cursor: pointer; }
  /* theme.css centers a desktop compare-row. Grade labels share a top edge instead. The 180px column is the theme's. */
  @media (min-width: 900px) {
    .titles { grid-template-columns: 180px repeat(2, minmax(0, 1fr)); }
    .titles .spacer { display: block; grid-row: span 4; }
    .compare-row { align-items: start; }
  }
</style>
