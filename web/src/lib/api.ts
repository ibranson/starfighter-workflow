// Thin typed wrapper over the daemon's JSON API. The SPA is served from the
// same origin as the API, so the session cookie rides along automatically;
// we only need to send the Origin header (the browser does) for CSRF.

export type Role = 'admin' | 'user';

export interface User {
  id: number;
  username: string;
  role: Role;
  display_name: string;
  created_at: string;
  last_login_at?: string;
}

export type Status = 'received' | 'in_repair' | 'awaiting_parts' | 'completed' | 'cancelled';

export type Priority = 'low' | 'normal' | 'high' | 'urgent';

export interface RepairRequest {
  id: number;
  machine_id: number;
  machine_name: string;
  problem_summary: string;
  problem_detail: string;
  status: Status;
  priority: Priority;
  assigned_to?: number;
  assigned_username?: string;
  created_by?: number;
  created_at: string;
  updated_at: string;
  closed_at?: string;
}

export interface Machine {
  id: number;
  name: string;
  created_at: string;
}

export interface StatusMeta {
  status: Status;
  terminal: boolean;
  next: Status[];
}

export interface Health {
  display_name: string;
  needs_setup: boolean;
  version: string;
  server_time: string;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `request failed (${res.status})`);
  }
  return data as T;
}

export const api = {
  status: () => req<Health>('GET', '/api/v1/status'),

  // Auth.
  me: () => req<{ user: User }>('GET', '/api/v1/auth/me'),
  login: (username: string, password: string) =>
    req<{ user: User }>('POST', '/api/v1/auth/login', { username, password }),
  setup: (username: string, password: string, display_name: string) =>
    req<{ user: User }>('POST', '/api/v1/setup', { username, password, display_name }),
  logout: () => req<unknown>('POST', '/api/v1/auth/logout'),
  changeMyPassword: (password: string) =>
    req<unknown>('POST', '/api/v1/auth/me/password', { password }),

  // Workflow metadata.
  workflowMeta: () =>
    req<{ statuses: StatusMeta[]; priorities: Priority[] }>('GET', '/api/v1/workflow/meta'),

  // Machines accumulator — type-ahead for the reporting form.
  searchMachines: (q = '') => {
    const qs = q ? `?q=${encodeURIComponent(q)}` : '';
    return req<{ machines: Machine[] }>('GET', `/api/v1/machines${qs}`);
  },

  // Repair requests.
  listRequests: (params: { status?: string; open?: boolean } = {}) => {
    const qs = new URLSearchParams();
    if (params.status) qs.set('status', params.status);
    if (params.open) qs.set('open', '1');
    const q = qs.toString();
    return req<{ requests: RepairRequest[] }>('GET', '/api/v1/requests' + (q ? `?${q}` : ''));
  },
  createRequest: (input: { machine: string; problem_summary: string; problem_detail?: string; priority?: Priority }) =>
    req<{ request: RepairRequest }>('POST', '/api/v1/requests', input),
  getRequest: (id: number) =>
    req<{ request: RepairRequest }>('GET', `/api/v1/requests/${id}`),
  // Claim = received -> in_repair, taking ownership. Throws ApiError(409) if
  // someone else already claimed it (first-wins) — callers must report that.
  claim: (id: number) =>
    req<{ request: RepairRequest }>('POST', `/api/v1/requests/${id}/claim`),
  // Take ownership of an already-owned request (pull-only).
  takeOver: (id: number) =>
    req<{ request: RepairRequest }>('POST', `/api/v1/requests/${id}/take-over`),
  transition: (id: number, status: Status) =>
    req<{ request: RepairRequest }>('POST', `/api/v1/requests/${id}/transition`, { status }),
  setPriority: (id: number, priority: Priority) =>
    req<{ request: RepairRequest }>('POST', `/api/v1/requests/${id}/priority`, { priority }),

  // User management (admin only).
  listUsers: () => req<{ users: User[] }>('GET', '/api/v1/users'),
  createUser: (username: string, password: string, role: Role, display_name = '') =>
    req<{ user: User }>('POST', '/api/v1/users', { username, password, role, display_name }),
  deleteUser: (id: number) => req<unknown>('DELETE', `/api/v1/users/${id}`),
  setUserRole: (id: number, role: Role) => req<unknown>('PATCH', `/api/v1/users/${id}`, { role }),
  setUserPassword: (id: number, password: string) =>
    req<unknown>('POST', `/api/v1/users/${id}/password`, { password })
};
