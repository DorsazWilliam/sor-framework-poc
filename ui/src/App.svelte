<script lang="ts">
  import { Tooltip } from "bits-ui";
  import Shell from "./shell/Shell.svelte";
  import { boot } from "./app/session.svelte";
  import { connect } from "./app/live.svelte";

  const booted = boot().then(connect);
</script>

<Tooltip.Provider delayDuration={400}>
  {#await booted then}
    <Shell />
  {:catch e}
    <p class="boot-error">{String(e)}</p>
  {/await}
</Tooltip.Provider>
