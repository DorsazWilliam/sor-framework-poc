<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";
  import ParsedInput from "./ParsedInput.svelte";
  import { fmtDuration, parseDuration } from "./format";

  let { value = $bindable(), ...rest }: Omit<HTMLInputAttributes, "value"> & { value?: number | null } = $props();
</script>

<ParsedInput bind:value parse={parseDuration} format={fmtDuration} placeholder="h:mm" {...rest} />
