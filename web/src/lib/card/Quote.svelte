<script lang="ts">
 import { sourceLabel, type Finding } from './contract';
 let { finding, prominent = false } = $props<{ finding: Finding; prominent?: boolean }>();
</script>
<figure class:prominent data-finding={finding.id}><blockquote>{finding.quote}</blockquote><figcaption><!-- Validated absolute external source URL, not an app route. -->
<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
<a href={finding.source.url} target="_blank" rel="noopener noreferrer">{sourceLabel(finding)}</a><span>{finding.stance === 'mitigation' ? 'Setting or control' : finding.stance === 'risk' ? 'Reported risk' : 'Supporting evidence'}</span></figcaption></figure>
<style>
 figure { margin: 16px 0 0; padding: 16px 18px; background: var(--raised); border-left: 2px solid var(--line); }
 blockquote { margin: 0; font-family: Georgia, Cambria, serif; font-size: 1.12rem; line-height: 1.5; color: var(--text); white-space: pre-wrap; }
 figcaption { display: flex; flex-wrap: wrap; align-items: center; gap: 0 16px; font-size: .78rem; color: var(--dim); margin-top: 8px; }
 a { display: inline-flex; align-items: center; min-height: 48px; }
 .prominent { margin: 0; padding: 0; background: transparent; border: 0; } .prominent blockquote { font-size: clamp(1.4rem, 2.3vw, 1.6rem); }
</style>
