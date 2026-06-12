<script lang="ts">
  import { session } from '$lib/session.svelte';
  import { api, ApiError, type User, type Role } from '$lib/api';

  let users = $state<User[]>([]);
  let error = $state('');

  // New-user form.
  let nu = $state({ username: '', password: '', display_name: '', role: 'user' as Role });
  let createError = $state('');

  async function load() {
    error = '';
    try { users = (await api.listUsers()).users; }
    catch (e) { error = e instanceof ApiError ? e.message : String(e); }
  }

  $effect(() => { if (session.isAdmin) load(); });

  async function createUser() {
    createError = '';
    try {
      await api.createUser(nu.username, nu.password, nu.role, nu.display_name);
      nu = { username: '', password: '', display_name: '', role: 'user' };
      await load();
    } catch (e) { createError = e instanceof ApiError ? e.message : String(e); }
  }

  async function setRole(u: User, role: Role) {
    error = '';
    try { await api.setUserRole(u.id, role); await load(); }
    catch (e) { error = e instanceof ApiError ? e.message : String(e); }
  }

  async function remove(u: User) {
    if (!confirm(`Delete user "${u.username}"?`)) return;
    error = '';
    try { await api.deleteUser(u.id); await load(); }
    catch (e) { error = e instanceof ApiError ? e.message : String(e); }
  }

  async function resetPassword(u: User) {
    const pw = prompt(`New password for "${u.username}" (min 8 chars):`);
    if (!pw) return;
    error = '';
    try { await api.setUserPassword(u.id, pw); }
    catch (e) { error = e instanceof ApiError ? e.message : String(e); }
  }
</script>

{#if !session.isAdmin}
  <div class="panel"><p>Admins only.</p><a href="/">← Back to requests</a></div>
{:else}
  <h2 style="margin-top: 0;">Users</h2>

  <div class="panel stack" style="margin-bottom: 1rem;">
    <strong>Add a user</strong>
    <div class="row" style="gap: 0.6rem; flex-wrap: wrap;">
      <label class="stack" style="flex:1; min-width: 140px;">Username<input bind:value={nu.username} /></label>
      <label class="stack" style="flex:1; min-width: 140px;">Display name<input bind:value={nu.display_name} /></label>
      <label class="stack" style="flex:1; min-width: 140px;">Password<input type="password" bind:value={nu.password} placeholder="min 8 chars" /></label>
      <label class="stack" style="width: 120px;">Role
        <select bind:value={nu.role}><option value="user">user</option><option value="admin">admin</option></select>
      </label>
    </div>
    {#if createError}<div class="error">{createError}</div>{/if}
    <button class="primary" onclick={createUser}>Create user</button>
  </div>

  {#if error}<div class="error" style="margin-bottom: 1rem;">{error}</div>{/if}

  <div class="stack">
    {#each users as u (u.id)}
      <div class="panel row" style="justify-content: space-between;">
        <div>
          <strong>{u.username}</strong>
          {#if u.display_name}<span class="muted"> · {u.display_name}</span>{/if}
          <span class="badge" style="margin-left: 0.4rem;">{u.role}</span>
        </div>
        <div class="row" style="gap: 0.4rem;">
          {#if u.role === 'user'}
            <button onclick={() => setRole(u, 'admin')}>Make admin</button>
          {:else}
            <button onclick={() => setRole(u, 'user')}>Make user</button>
          {/if}
          <button onclick={() => resetPassword(u)}>Reset password</button>
          {#if u.id !== session.user?.id}
            <button class="danger" onclick={() => remove(u)}>Delete</button>
          {/if}
        </div>
      </div>
    {/each}
  </div>
{/if}
