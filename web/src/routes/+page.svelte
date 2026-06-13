<script lang="ts">
  import { session } from '$lib/session.svelte';
  import {
    api, ApiError,
    type RepairRequest, type StatusMeta, type Status, type Priority
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
  const labelFor = (s: string) => s.replace(/_/g, ' ');
  const myId = $derived(session.user?.id ?? -1);
  const ownedByMe = (r: RepairRequest) => r.assigned_to === myId;

  async function loadAll() {
    if (!session.user) return;
    listError = '';
    try {
      const [reqs, wf] = await Promise.all([api.listRequests({ open: openOnly }), api.workflowMeta()]);
      requests = reqs.requests;
      meta = wf.statuses;
      priorities = wf.priorities;
      // keep the open detail in sync if it's still in the list
      if (selected) selected = requests.find((r) => r.id === selected!.id) ?? selected;
    } catch (e) {
      listError = e instanceof ApiError ? e.message : String(e);
    }
  }

  $effect(() => { openOnly; if (session.user) loadAll(); });

  // ---- create form ----
  let nf = $state({ machine: '', problem_summary: '', problem_detail: '', priority: 'normal' as Priority });
  let createError = $state('');
  let showCreate = $state(false);

  // Machine type-ahead: as the user types, fetch existing names from the
  // accumulator to suggest. Free text is allowed — submitting an unknown name
  // creates (accumulates) it server-side via find-or-create.
  let machineSuggestions = $state<string[]>([]);
  async function onMachineInput() {
    try {
      const { machines } = await api.searchMachines(nf.machine);
      machineSuggestions = machines.map((m) => m.name);
    } catch { /* suggestions are best-effort */ }
  }

  async function createRequest() {
    createError = '';
    try {
      await api.createRequest({ ...nf });
      nf = { machine: '', problem_summary: '', problem_detail: '', priority: 'normal' };
      machineSuggestions = [];
      showCreate = false;
      await loadAll();
    } catch (e) { createError = e instanceof ApiError ? e.message : String(e); }
  }

  // ---- detail / actions ----
  let selected = $state<RepairRequest | null>(null);
  let detailError = $state('');

  function selectRequest(r: RepairRequest) { detailError = ''; selected = r; }

  // Refresh just the selected request after an action (and the list behind it).
  async function refreshSelected(updated?: RepairRequest) {
    if (updated) selected = updated;
    await loadAll();
  }

  async function claim(r: RepairRequest) {
    detailError = '';
    try { const { request } = await api.claim(r.id); await refreshSelected(request); }
    catch (e) {
      // First-wins: a 409 here means someone else already claimed it. Report
      // it and refresh so the board shows the real owner.
      detailError = e instanceof ApiError ? e.message : String(e);
      await loadAll();
    }
  }
  async function takeOver(r: RepairRequest) {
    detailError = '';
    try { const { request } = await api.takeOver(r.id); await refreshSelected(request); }
    catch (e) { detailError = e instanceof ApiError ? e.message : String(e); await loadAll(); }
  }
  async function transition(r: RepairRequest, to: Status) {
    detailError = '';
    try { const { request } = await api.transition(r.id, to); await refreshSelected(request); }
    catch (e) { detailError = e instanceof ApiError ? e.message : String(e); await loadAll(); }
  }
  async function changePriority(r: RepairRequest, p: Priority) {
    detailError = '';
    try { const { request } = await api.setPriority(r.id, p); await refreshSelected(request); }
    catch (e) { detailError = e instanceof ApiError ? e.message : String(e); }
  }

  const fmt = (iso: string) => new Date(iso).toLocaleString();
</script>

{#if session.health?.needs_setup}
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
      <button class="primary" onclick={() => (showCreate = !showCreate)}>{showCreate ? 'Cancel' : '+ Log a fault'}</button>
    </div>
  </div>

  {#if showCreate}
    <div class="panel stack" style="margin-bottom: 1rem;">
      <div class="row" style="gap: 0.6rem;">
        <label class="stack" style="flex:1;">Machine*
          <input bind:value={nf.machine} oninput={onMachineInput} onfocus={onMachineInput}
                 list="machine-suggestions" autocomplete="off" placeholder="start typing — pick one or add new" />
          <datalist id="machine-suggestions">
            {#each machineSuggestions as name}<option value={name}></option>{/each}
          </datalist>
        </label>
        <label class="stack" style="width: 130px;">Priority
          <select bind:value={nf.priority}>{#each priorities as p}<option value={p}>{p}</option>{/each}</select>
        </label>
      </div>
      <label class="stack">Problem summary*<input bind:value={nf.problem_summary} placeholder="Right flipper weak" /></label>
      <label class="stack">Details<textarea rows="3" bind:value={nf.problem_detail}></textarea></label>
      {#if createError}<div class="error">{createError}</div>{/if}
      <button class="primary" onclick={createRequest}>Log it</button>
    </div>
  {/if}

  {#if listError}<div class="error" style="margin-bottom: 1rem;">{listError}</div>{/if}

  <div class="stack">
    {#each requests as r (r.id)}
      <button class="panel" style="text-align: left; width: 100%;" onclick={() => selectRequest(r)}>
        <div class="row" style="justify-content: space-between;">
          <strong>#{r.id} · {r.machine_name}</strong>
          <span class="row" style="gap: 0.4rem;">
            <span class="badge prio-{r.priority}">{r.priority}</span>
            <span class="badge">{labelFor(r.status)}</span>
          </span>
        </div>
        <div class="row" style="justify-content: space-between;">
          <span class="muted" style="font-size: 0.9rem;">{r.problem_summary}</span>
          <span class="muted" style="font-size: 0.85rem;">
            {#if r.assigned_username}owned by {r.assigned_username}{:else if r.status === 'received'}unclaimed{/if}
          </span>
        </div>
      </button>
    {:else}
      <p class="muted">No {openOnly ? 'open ' : ''}requests yet.</p>
    {/each}
  </div>

  {#if selected}
    {@const m = metaFor(selected.status)}
    {@const terminal = m?.terminal ?? false}
    <div class="panel stack" style="margin-top: 1.2rem; border-color: var(--accent);">
      <div class="row" style="justify-content: space-between;">
        <h3 style="margin: 0;">#{selected.id} · {selected.machine_name}</h3>
        <button onclick={() => (selected = null)}>Close</button>
      </div>
      <div class="row" style="gap: 0.5rem; flex-wrap: wrap;">
        <span class="badge">{labelFor(selected.status)}</span>
        <span class="badge prio-{selected.priority}">priority: {selected.priority}</span>
        <span class="muted">
          {#if selected.assigned_username}owner: {selected.assigned_username}{ownedByMe(selected) ? ' (you)' : ''}
          {:else if selected.status === 'received'}unclaimed{/if}
        </span>
      </div>
      {#if selected.problem_detail}<p style="margin: 0;">{selected.problem_detail}</p>{/if}
      <p class="muted" style="margin: 0; font-size: 0.85rem;">logged {fmt(selected.created_at)} · updated {fmt(selected.updated_at)}{selected.closed_at ? ` · closed ${fmt(selected.closed_at)}` : ''}</p>

      <!-- Actions, driven by the state machine + ownership. -->
      {#if terminal}
        <p class="muted">Closed ({labelFor(selected.status)}) — no further actions.</p>
      {:else}
        <div class="row" style="flex-wrap: wrap; gap: 0.4rem; align-items: center;">
          {#if selected.status === 'received'}
            <button class="primary" onclick={() => claim(selected!)}>Claim (take ownership)</button>
            <button class="danger" onclick={() => transition(selected!, 'cancelled')}>Cancel</button>
          {:else}
            {#if !ownedByMe(selected)}
              <button class="primary" onclick={() => takeOver(selected!)}>Take ownership</button>
            {/if}
            {#each (m?.next ?? []) as to}
              {#if to === 'cancelled'}
                <button class="danger" onclick={() => transition(selected!, to)}>Cancel</button>
              {:else}
                <button onclick={() => transition(selected!, to)}>→ {labelFor(to)}</button>
              {/if}
            {/each}
          {/if}
        </div>

        <label class="row" style="gap: 0.4rem;">Priority:
          <select style="width:auto" value={selected.priority} onchange={(e) => changePriority(selected!, (e.currentTarget as HTMLSelectElement).value as Priority)}>
            {#each priorities as p}<option value={p}>{p}</option>{/each}
          </select>
        </label>
      {/if}

      {#if detailError}<div class="error">{detailError}</div>{/if}
    </div>
  {/if}
{/if}
