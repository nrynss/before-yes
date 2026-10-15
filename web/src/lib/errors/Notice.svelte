<script lang="ts">
  import { errorNotice, unknownAreaNotice } from './notice';
  let { error = undefined, unknownArea = false } = $props<{ error?: unknown; unknownArea?: boolean }>();
  const shown = $derived(unknownArea ? unknownAreaNotice() : errorNotice(error));
</script>
<section class="notice" class:unknown={unknownArea} role={unknownArea ? 'status' : 'alert'}>
  <p class="situation">{shown.situation}</p>
  <p class="next">{shown.next}</p>
</section>
<style>
  .notice { max-width: 40rem; color: var(--text, #172c25); font-family: var(--font-ui, system-ui, sans-serif); background: var(--stop-soft, #f7e4de); border-left: 3px solid var(--stop, #a83f35); padding: 16px 18px; border-radius: var(--radius-panel, 14px); }
  .unknown { background: var(--warn-soft, #f4e9ce); border-left-color: var(--warn, #82590f); }
  p { margin: 0; overflow-wrap: anywhere; }
  .next { margin-top: 8px; }
</style>
