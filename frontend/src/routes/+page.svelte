<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { tokenStore } from '$lib/stores';
  import { get } from 'svelte/store';

  onMount(() => {
    const t = get(tokenStore);
    goto(t ? '/inventory' : '/auth', { replaceState: true });
  });
</script>
