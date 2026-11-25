<script lang="ts">
  import type { Snippet } from "svelte";
  import type { Col } from "../api/contract";
  import FieldEditor from "./FieldEditor.svelte";
  import { colLabel } from "../app/session.svelte";

  // A screen's params bar: one field per param, then the screen's own controls (children, styled `.param` for a field).
  let {
    cols,
    params = $bindable(),
    id,
    nested = false,
    children,
  }: { cols: Col[]; params: Record<string, unknown>; id: string; nested?: boolean; children?: Snippet } = $props();
</script>

<div class="params" class:nested>
  {#each cols as p (p.name)}
    <label class="param" class:wide={!!p.ref}>
      <span>{colLabel(p.name)}</span>
      <FieldEditor col={p} id="p-{id}-{p.name}" bind:value={params[p.name]} />
    </label>
  {/each}
  {@render children?.()}
</div>

<style>
  .params {
    display: flex;
    align-items: center;
    gap: var(--s-5);
    min-height: var(--h-params);
    padding: var(--s-1) var(--s-4) var(--s-1) var(--s-6);
    border-bottom: var(--bw) solid var(--border);
    background: var(--surface);
  }
  .params.nested {
    padding-left: var(--s-4);
  }
  .params :global(.param) {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    width: 210px;
    white-space: nowrap;
  }
  .params :global(.param.wide) {
    width: 250px;
  }
  .params :global(.param > span) {
    color: var(--text-muted);
  }
  .params :global(.spacer) {
    flex: 1;
  }
</style>
