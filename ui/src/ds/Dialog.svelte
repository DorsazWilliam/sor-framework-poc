<script lang="ts">
  import type { Snippet } from "svelte";
  import { Dialog } from "bits-ui";

  let {
    open = $bindable(false),
    title,
    children,
    footer,
  }: { open?: boolean; title: string; children: Snippet; footer?: Snippet } = $props();
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="scrim" />
    <Dialog.Content class="dialog">
      <Dialog.Title class="dialog-title">{title}</Dialog.Title>
      <div class="dialog-body">{@render children()}</div>
      {#if footer}<div class="dialog-footer">{@render footer()}</div>{/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
