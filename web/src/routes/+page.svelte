<script lang="ts">
  import { session } from '$lib/session.svelte';
  import {
    api, ApiError,
    type RepairRequest, type RequestEvent, type StatusMeta, type Status, type Priority
  } from '$lib/api';

  // ---- auth forms ----
  let username = $state('');
  let password = $state('');
  let displayName = $state('');
  let authError = $state('');
  let busy = $state(false);

  async function submitLogin() {
    authError = ''; busy = true;
    try { await session.login(username, password); await loadAll(); }
    catch (e) { authError = e instanceof ApiError ? e.message : String(e); }
    finally { busy = false; password = ''; }
  }
  async function submitSetup() {
    authError = ''; busy = true;
    try { await session.setup(username, password, displayName); await loadAll(); }
    catch (e) { authError = e instanceof ApiError ? e.message : String(e); }
    finally { busy = false; password = ''; }
  }

  // ---- dashboard data ----
  let requests = $state<RepairRequest[]>([]);
  let meta = $state<StatusMeta[]>([]);
  let priorities = $state<Priority[]>(['low', 'normal', 'high', 'urgent']);
  let openOnly = $state(true);
  let listError = $state('');

  const metaFor = (s: Status) => meta.find((m) => m.status === s);

  async function loadAll() {
    if (!session.user) return;
    listError = '';
    try {
      const [reqs, wf] = await Promise.all([
        api.listRequests({ open: openOnly }),
        api.workflowMeta()
      ]);
      requests = reqs.requests;
      meta = wf.statuses;
      priorities = wf.priorities;
    } catch (e) {
      listError = e instanceof ApiError ? e.message : String(e);
    }
  }

  // Reload when the open-only filter flips, once authed.
  $effect(() => { openOnly; if (session.user) loadAll(); });

  // ---- create form ----
  let nf = $state({ game_title: '', cabinet_ref: '', problem_summary: '', problem_detail: '', reporter_name: '', reporter_contact: '', priority: 'normal' as Priority });
  let createError = $state('');
  let showCreate = $state(false);

  async function createRequest() {
    createError = '';
    try {
      await api.createRequest({ ...nf });
      nf = { game_title: '', cabinet_ref: '', problem_summary: '', problem_detail: '', reporter_name: '', reporter_contact: '', priority: 'normal' };
      showCreate = false;
      await loadAll();
    } catch (e) {
      createError = e instanceof ApiError ? e.message : String(e);
    }
  }

  // ---- detail / transitions ----
  let selected = $state<RepairRequest | null>(null);
  let events = $state<RequestEvent[]>([]);
  let detailNote = $state('');
  let detailError = $state('');

  async function openDetail(r: RepairRequest) {
    detailError = ''; detailNote = '';
    const { request, events: evs } = await api.getRequest(r.id);
    selected = request; events = evs;
  }
  async function transition(to: Status) {
    if (!selected) return;
    detailError = '';
    try {
      await api.transition(selected.id, to, detailNote);
      await openDetail(selected); await loadAll(); detailNote = '';
    } catch (e) { detailError = e instanceof ApiError ? e.message : String(e); }
  }
  async function addNote() {
    if (!selected || !detailNote.trim()) return;
    try { await api.addNote(selected.id, detailNote); await openDetail(selected); detailNote = ''; }
    catch (e) { detailError = e instanceof ApiError ? e.message : String(e); }
  }
  async function changePriority(p: Priority) {
    if (!selected) return;
    try { await api.setPriority(selected.id, p); await openDetail(selected); await loadAll(); }
    catch (e) { detailError = e instanceof ApiError ? e.message : String(e); }
  }

  const fmt = (iso: string) => new Date(iso).toLocaleString();
  const labelFor = (s: string) => s.replace(/_/g, ' ');
</script>

{#if session.health?.needs_setup}
  <!-- First-boot: create the initial admin. -->
  <div class="panel stack" style="max-width: 420px; margin: 3rem auto;">
    <h2 style="margin: 0;">Welcome — create the first admin</h2>
    <p class="muted" style="margin: 0;">This account can manage users. You can add more later.</p>
    <label class="stack">Display name<input bind:value={displayName} placeholder="e.g. Shop Lead" /></label>
    <label class="stack">Username<input bind:value={username} autocomplete="username" /></label>
    <label class="stack">Password<input type="password" bind:value={password} autocomplete="new-password" placeholder="min 8 characters" /></label>
    {#if authError}<div class="error">{authError}</div>{/if}
    <button class="primary" disabled={busy} onclick={submitSetup}>Create admin & sign in</button>
  </div>
{:else if !session.user}
  <!-- Login. -->
  <div class="panel stack" style="max-width: 380px; margin: 3rem auto;">
    <h2 style="margin: 0;">Sign in</h2>
    <label class="stack">Username<input bind:value={username} autocomplete="username" /></label>
    <label class="stack">Password<input type="password" bind:value={password} autocomplete="current-password" /></label>
    {#if authError}<div class="error">{authError}</div>{/if}
    <button class="primary" disabled={busy} onclick={submitLogin}>Sign in</button>
  </div>
{:else}
  <!-- Dashboard. -->
  <div class="row" style="justify-content: space-between; margin-bottom: 1rem;">
    <h2 style="margin: 0;">Repair requests</h2>
    <div class="row">
      <label class="row" style="gap: 0.3rem; font-size: 0.9rem;"><input type="checkbox" style="width:auto" bind:checked={openOnly} /> open only</label>
      <button class="primary" onclick={() => (showCreate = !showCreate)}>{showCreate ? 'Cancel' : '+ New request'}</button>
    </div>
  </div>

  {#if showCreate}
    <div class="panel stack" style="margin-bottom: 1rem;">
      <div class="row" style="gap: 0.6rem;">
        <label class="stack" style="flex:1;">Game title*<input bind:value={nf.game_title} placeholder="Galaga" /></label>
        <label class="stack" style="flex:1;">Cabinet ref<input bind:value={nf.cabinet_ref} placeholder="serial / location" /></label>
        <label class="stack" style="width: 130px;">Priority
          <select bind:value={nf.priority}>{#each priorities as p}<option value={p}>{p}</option>{/each}</select>
        </label>
      </div>
      <label class="stack">Problem summary*<input bind:value={nf.problem_summary} placeholder="No coin drop on P1" /></label>
      <label class="stack">Details<textarea rows="3" bind:value={nf.problem_detail}></textarea></label>
      <div class="row" style="gap: 0.6rem;">
        <label class="stack" style="flex:1;">Reporter<input bind:value={nf.reporter_name} /></label>
        <label class="stack" style="flex:1;">Contact<input bind:value={nf.reporter_contact} placeholder="phone / email" /></label>
      </div>
      {#if createError}<div class="error">{createError}</div>{/if}
      <button class="primary" onclick={createRequest}>Create</button>
    </div>
  {/if}

  {#if listError}<div class="error" style="margin-bottom: 1rem;">{listError}</div>{/if}

  <div class="stack">
    {#each requests as r (r.id)}
      <button class="panel" style="text-align: left; width: 100%;" onclick={() => openDetail(r)}>
        <div class="row" style="justify-content: space-between;">
          <strong>#{r.id} · {r.game_title}</strong>
          <span class="row" style="gap: 0.4rem;">
            <span class="badge prio-{r.priority}">{r.priority}</span>
            <span class="badge">{labelFor(r.status)}</span>
          </span>
        </div>
        <div class="muted" style="font-size: 0.9rem;">{r.problem_summary}</div>
      </button>
    {:else}
      <p class="muted">No {openOnly ? 'open ' : ''}requests yet.</p>
    {/each}
  </div>

  {#if selected}
    {@const m = metaFor(selected.status)}
    <div class="panel stack" style="margin-top: 1.2rem; border-color: var(--accent);">
      <div class="row" style="justify-content: space-between;">
        <h3 style="margin: 0;">#{selected.id} · {selected.game_title}</h3>
        <button onclick={() => (selected = null)}>Close</button>
      </div>
      <div class="row" style="gap: 0.5rem; flex-wrap: wrap;">
        <span class="badge">{labelFor(selected.status)}</span>
        <span class="badge prio-{selected.priority}">priority: {selected.priority}</span>
        {#if selected.cabinet_ref}<span class="muted">cabinet: {selected.cabinet_ref}</span>{/if}
      </div>
      {#if selected.problem_detail}<p style="margin: 0;">{selected.problem_detail}</p>{/if}
      {#if selected.reporter_name || selected.reporter_contact}
        <p class="muted" style="margin: 0;">Reported by {selected.reporter_name} {selected.reporter_contact ? `· ${selected.reporter_contact}` : ''}</p>
      {/if}

      <label class="stack">Note (attached to the next action)
        <textarea rows="2" bind:value={detailNote} placeholder="optional note…"></textarea>
      </label>

      <div class="row" style="flex-wrap: wrap; gap: 0.4rem;">
        {#if m && m.next.length}
          <span class="muted" style="align-self:center;">Move to:</span>
          {#each m.next as to}
            <button onclick={() => transition(to)}>{labelFor(to)}</button>
          {/each}
        {:else}
          <span class="muted">Terminal state — no further transitions.</span>
        {/if}
        <button onclick={addNote} disabled={!detailNote.trim()}>Add note</button>
      </div>

      <label class="row" style="gap: 0.4rem;">Priority:
        <select style="width:auto" value={selected.priority} onchange={(e) => changePriority((e.currentTarget as HTMLSelectElement).value as Priority)}>
          {#each priorities as p}<option value={p}>{p}</option>{/each}
        </select>
      </label>

      {#if detailError}<div class="error">{detailError}</div>{/if}

      <h4 style="margin: 0.5rem 0 0;">History</h4>
      <div class="stack" style="gap: 0.3rem;">
        {#each events as ev}
          <div style="font-size: 0.88rem; border-left: 2px solid var(--border); padding-left: 0.6rem;">
            <span class="muted">{fmt(ev.created_at)} · </span>
            {#if ev.kind === 'status_change'}{labelFor(ev.from_value ?? '')} → {labelFor(ev.to_value ?? '')}
            {:else if ev.kind === 'created'}created
            {:else if ev.kind === 'priority_change'}priority {ev.from_value} → {ev.to_value}
            {:else if ev.kind === 'assignment'}reassigned
            {:else}{ev.kind}{/if}
            {#if ev.note}<span> — {ev.note}</span>{/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}
{/if}
