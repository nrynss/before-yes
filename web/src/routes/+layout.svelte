<script lang="ts">
 import { resolve } from '$app/paths';
 import { onMount } from 'svelte';
 import { theme, type ThemeMode } from '@nrynss/chaaya/theme';
 import '$lib/theme/theme.css';
 let { children } = $props();
 let mode = $state<ThemeMode>('system');
 onMount(() => { mode = theme.mode; });
 function choose(event: Event) {
  mode = (event.currentTarget as HTMLSelectElement).value as ThemeMode;
  theme.set(mode);
 }
</script>
<svelte:head><title>Before Yes — a second look for parents</title><meta name="description" content="Evidence-linked guidance for the apps, games and channels your child asks for." /></svelte:head>
<a class="skip" href="#main">Skip to content</a>
<header class="masthead"><a class="wordmark" href={resolve('/')}><span class="mark" aria-hidden="true">↗</span>Before Yes<span class="wordmark-dot" aria-hidden="true">.</span></a><label class="theme-control"><span>Theme</span><select aria-label="Theme" value={mode} onchange={choose}><option value="system">System</option><option value="light">Light</option><option value="dark">Dark</option></select></label></header>
<main id="main" tabindex="-1">{@render children()}</main>
<footer><span>For the questions that come before yes.</span><span>Every flag should lead to a source.</span></footer>
