<script lang="ts">
  import { Checkbox } from "bits-ui";
  import Check from "~icons/fluent/checkmark-12-regular";
  import Dash from "~icons/fluent/subtract-12-regular";

  let {
    checked = $bindable(false),
    indeterminate = false,
    id,
    label,
    onCheckedChange,
  }: {
    checked?: boolean;
    indeterminate?: boolean;
    id?: string;
    label?: string;
    onCheckedChange?: (v: boolean) => void;
  } = $props();
</script>

<Checkbox.Root class="checkbox" bind:checked {indeterminate} {id} aria-label={label} {onCheckedChange}>
  {#snippet children({ checked, indeterminate })}
    {#if indeterminate}<Dash />{:else if checked}<Check />{/if}
  {/snippet}
</Checkbox.Root>
