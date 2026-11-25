<script lang="ts">
  import Error from "~icons/fluent/error-circle-16-filled";
  import Success from "~icons/fluent/checkmark-circle-16-filled";
  import Info from "~icons/fluent/info-16-filled";
  import Close from "~icons/fluent/dismiss-16-regular";
  import IconButton from "./IconButton.svelte";
  import { dismiss, toasts } from "./toast.svelte";
  import { t } from "../labels/i18n.svelte";

  const icons = { error: Error, success: Success, info: Info };
</script>

<div class="toaster" role="status" aria-live="polite">
  {#each toasts as x (x.id)}
    {@const I = icons[x.kind]}
    <div class="toast" data-kind={x.kind}>
      <I />
      <div>
        <div class="toast-title">{x.title}</div>
        {#if x.lines?.length}<ul>{#each x.lines as l, i (i)}<li>{l}</li>{/each}</ul>{/if}
      </div>
      <IconButton icon={Close} label={t("ui.close")} onclick={() => dismiss(x.id)} />
    </div>
  {/each}
</div>
