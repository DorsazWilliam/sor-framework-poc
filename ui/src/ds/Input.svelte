<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";

  let { value = $bindable(""), ...rest }: HTMLInputAttributes = $props();
</script>

<input class="input" autocomplete="off" bind:value {...rest} />
