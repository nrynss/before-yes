<script lang="ts">
 import { page } from '$app/state';
 import Progress from '$lib/progress/Progress.svelte';
</script>
{#key page.params.id}<Progress id={page.params.id ?? ''} />{/key}
