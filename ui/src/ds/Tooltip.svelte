<script lang="ts">
  import type { Snippet } from "svelte";
  import { Tooltip } from "bits-ui";

  let { text, content, children }: { text?: string; content?: Snippet; children: Snippet } = $props();
</script>

{#if text || content}
  <Tooltip.Root>
    <Tooltip.Trigger>
      {#snippet child({ props })}
        <span {...props} class="tip-anchor">{@render children()}</span>
      {/snippet}
    </Tooltip.Trigger>
    <Tooltip.Portal>
      <Tooltip.Content class="tooltip" sideOffset={6}>
        {#if content}{@render content()}{:else}{text}{/if}
      </Tooltip.Content>
    </Tooltip.Portal>
  </Tooltip.Root>
{:else}
  {@render children()}
{/if}
