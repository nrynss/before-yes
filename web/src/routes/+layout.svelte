<script lang="ts">
 import { resolve } from '$app/paths';
 import { onMount } from 'svelte';
 import { theme, type ThemeMode } from '@nrynss/chaaya/theme';
 import '$lib/theme/theme.css';
 let { children } = $props();
 let mode = $state<ThemeMode>('system');
 onMount(() => { mode = theme.mode; });
 const labels = { system: 'System', light: 'Light', dark: 'Dark' };
 const next = { system: 'light', light: 'dark', dark: 'system' } as const;
 function choose() {
  mode = next[mode];
  theme.set(mode);
 }
</script>
<svelte:head><title>Before Yes – A research tool for parents</title><meta name="description" content="Evidence-linked guidance for the apps, games and channels your child asks for." /></svelte:head>
<a class="skip" href="#main">Skip to content</a>
<header class="masthead"><a class="wordmark" href={resolve('/')}><span class="mark" aria-hidden="true">↗</span>Before Yes<span class="wordmark-dot" aria-hidden="true">.</span></a><button class="theme-toggle" type="button" onclick={choose} aria-label={`Theme: ${labels[mode]}. Switch to ${labels[next[mode]]}.`} data-mode={mode}><span class="theme-icon" class:monitor={mode === 'system'} aria-hidden="true">{mode === 'light' ? '☀' : mode === 'dark' ? '☾' : ''}</span></button></header>
<main id="main" tabindex="-1">{@render children()}</main>
<footer><span>Every concern links to its source.</span></footer>

<style>
 .theme-toggle { display: inline-flex; align-items: center; justify-content: center; width: 44px; height: 44px; padding: 10px; border: 0; border-radius: var(--radius-control); background: transparent; color: var(--text); font: inherit; font-size: .8rem; cursor: pointer; }
 .theme-toggle:hover { background: var(--surface); }
 .theme-icon { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px; font-size: 24px; line-height: 1; }
 .monitor { position: relative; width: 18px; height: 13px; border: 1.5px solid currentColor; border-radius: 2px; margin: 0 1px 4px; }
 .monitor::after { content: ''; position: absolute; width: 10px; height: 4px; border-bottom: 1.5px solid currentColor; bottom: -5px; left: 2.5px; }
 .monitor::before { content: ''; position: absolute; height: 4px; border-left: 1.5px solid currentColor; bottom: -4px; left: 7px; }
</style>
