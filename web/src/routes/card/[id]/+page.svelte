<script lang="ts">
 import { page } from '$app/state';
 import Card from '$lib/card/Card.svelte';
</script>
{#key page.params.id}<Card id={page.params.id ?? ''} />{/key}
