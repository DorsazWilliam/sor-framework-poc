<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";
  import ParsedInput from "./ParsedInput.svelte";
  import { fmtMoney, fmtNumber, parseNumber } from "./format";

  let {
    value = $bindable(),
    money = false,
    ...rest
  }: Omit<HTMLInputAttributes, "value"> & { value?: number | null; money?: boolean } = $props();
</script>

<ParsedInput bind:value parse={parseNumber} format={money ? fmtMoney : fmtNumber} {...rest} />
