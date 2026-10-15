<script lang="ts">
 import { page } from '$app/state';
 import Compare from '$lib/compare/Compare.svelte';
</script>
{#key `${page.params.a}/${page.params.b}`}<Compare a={page.params.a ?? ''} b={page.params.b ?? ''} />{/key}
