<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";
  import ParsedInput from "./ParsedInput.svelte";
  import { fmtTime, parseTime } from "./format";

  let { value = $bindable(), ...rest }: Omit<HTMLInputAttributes, "value"> & { value?: number | null } = $props();
</script>

<ParsedInput bind:value parse={parseTime} format={fmtTime} placeholder="hh:mm" {...rest} />
