<svelte:head><title>Before Yes – A research tool for parents</title></svelte:head>
<div class="ask-layout"><section class="hero"><p class="eyebrow">A research tool for parents</p><h1>Research first.<br />Yes next.</h1><p class="intro">Enter an app, game or YouTube channel and your child’s age. See what the sources say about chat, purchases, privacy and more.</p><Ask /></section><aside class="method" aria-labelledby="method"><h2 id="method">How a check works</h2><ol class="steps"><li><span class="step-number">01</span><div><h3>Find sources</h3><p>We look for policies, reviews, parent discussions and news about the title.</p></div></li><li><span class="step-number">02</span><div><h3>Check the evidence</h3><p>We read the sources for risks and parental controls, keeping the quotes behind each finding.</p></div></li><li><span class="step-number">03</span><div><h3>Get an age-specific answer</h3><p>We assess six areas for your child’s age and show the concerns, settings and gaps in the evidence.</p></div></li></ol></aside></div>
<section class="checklist" aria-labelledby="questions"><p class="eyebrow" id="questions">What we check</p><ul>{#each ['Contact with strangers','Purchases and spending','Privacy and data','Age restrictions','Content','Developer history'] as area (area)}<li>{area}</li>{/each}</ul></section>
<script lang="ts">import Ask from '$lib/ask/Ask.svelte';</script>
<style>
  @media (max-width: 480px) {
    .hero > .eyebrow { margin-bottom: 18px; }
    h1 { margin-bottom: 20px; }
    .intro { font-size: 1rem; margin-bottom: 26px; }
  }
</style>
