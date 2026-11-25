<script lang="ts">
  import { Tooltip } from "bits-ui";
  import Shell from "./shell/Shell.svelte";
  import { boot } from "./app/session.svelte";
  import { clear } from "./app/cache.svelte";
  import { connect } from "./app/live.svelte";
  import { ApiError } from "./api/client";
  import { t } from "./labels/i18n.svelte";

  const booted = boot().then(connect);
</script>

<Tooltip.Provider delayDuration={400}>
  {#await booted then}
    <Shell />
  {:catch e}
    <div class="boot-error" role="alert">
      {#if e instanceof ApiError && e.code === "forbidden"}
        <h1>{t("ui.unknown_user")}</h1>
        <p>{t("ui.unknown_user_hint")}</p>
      {:else}
        <h1>{t("ui.boot_failed")}</h1>
        <p>{e instanceof ApiError ? (e.problem.detail ?? e.problem.title) : String(e)}</p>
      {/if}
      <a class="btn" href="/auth/logout" onclick={clear}>{t("ui.logout")}</a>
    </div>
  {/await}
</Tooltip.Provider>

<style>
  .boot-error {
    max-width: 480px;
    margin: 15vh auto 0;
    padding: var(--s-8);
    border: var(--bw) solid var(--border);
    border-radius: var(--r-dialog);
    background: var(--surface);
  }
  h1 {
    margin: 0 0 var(--s-4);
    font-size: var(--fs-title);
    font-weight: var(--fw-semibold);
  }
  p {
    margin: 0 0 var(--s-7);
    color: var(--text-muted);
  }
  .btn {
    color: var(--accent);
  }
</style>
