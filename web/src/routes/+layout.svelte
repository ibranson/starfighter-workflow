<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { session } from '$lib/session.svelte';

  let { children } = $props();

  onMount(() => session.refresh());

  async function doLogout() {
    await session.logout();
  }
</script>

<header style="border-bottom: 1px solid var(--border); padding: 0.7rem 1rem;">
  <div style="max-width: 980px; margin: 0 auto; display: flex; align-items: center; gap: 1rem;">
    <strong style="font-size: 1.05rem;">
      🛠️ {session.health?.display_name ?? 'Repair Workflow'}
    </strong>
    <nav class="row" style="gap: 0.8rem; font-size: 0.92rem;">
      <a href="/">Requests</a>
      {#if session.isAdmin}<a href="/users">Users</a>{/if}
    </nav>
    <span style="flex: 1;"></span>
    {#if session.user}
      <span class="muted">{session.user.display_name || session.user.username}</span>
      <span class="badge">{session.user.role}</span>
      <button onclick={doLogout}>Log out</button>
    {/if}
  </div>
</header>

<main style="max-width: 980px; margin: 1.2rem auto; padding: 0 1rem;">
  {#if session.loading}
    <p class="muted">Loading…</p>
  {:else}
    {@render children()}
  {/if}
</main>
