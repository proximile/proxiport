<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { tokenStore } from '#lib/stores.ts';
  import { get } from 'svelte/store';

  onMount(() => {
    const t = get(tokenStore);
    goto(t ? '/inventory' : '/auth', { replace: true });
  });
</script>
