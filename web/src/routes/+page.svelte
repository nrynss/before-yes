<svelte:head><title>Before Yes — a second look for parents</title></svelte:head>
<div class="ask-layout"><section class="hero"><p class="eyebrow">A second look for parents</p><h1>A little research.<br />Before a big yes.</h1><p class="intro">An app, a game, a channel. A question from your child.<br class="desktop-break" /> Make room for an answer you can check.</p><Ask /></section><aside class="method" aria-labelledby="method"><p class="eyebrow">How a check works</p><h2 id="method">From question<br />to clarity.</h2><ol class="steps"><li><span class="step-number">01</span><div><h3>Gather</h3><p>Policies, parent reports and the sources behind them.</p></div></li><li><span class="step-number">02</span><div><h3>Read</h3><p>Look for the details that matter, with their exact words.</p></div></li><li><span class="step-number">03</span><div><h3>Judge</h3><p>The same six questions, considered for your child’s age.</p></div></li></ol></aside></div>
<section class="checklist" aria-labelledby="questions"><p class="eyebrow" id="questions">Six questions. Every title.</p><ul>{#each ['Contact','Money','Data','Age gating','Content','Developer record'] as area (area)}<li>{area}</li>{/each}</ul></section>
<script lang="ts">import Ask from '$lib/ask/Ask.svelte';</script>
<style>
  @media (max-width: 480px) {
    .hero > .eyebrow { margin-bottom: 18px; }
    h1 { margin-bottom: 20px; }
    .intro { font-size: 1rem; margin-bottom: 26px; }
  }
</style>
