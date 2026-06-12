// Global session/auth state as Svelte 5 runes. Imported wherever the UI needs
// to know who is logged in or whether the instance still needs first-boot
// setup.
import { api, type Health, type User } from './api';

class Session {
  user = $state<User | null>(null);
  health = $state<Health | null>(null);
  loading = $state(true);

  get isAdmin() {
    return this.user?.role === 'admin';
  }

  /** Refresh server status and the current user (if any). */
  async refresh() {
    this.loading = true;
    try {
      this.health = await api.status();
      if (!this.health.needs_setup) {
        try {
          const { user } = await api.me();
          this.user = user;
        } catch {
          this.user = null; // not logged in — expected
        }
      }
    } finally {
      this.loading = false;
    }
  }

  async login(username: string, password: string) {
    const { user } = await api.login(username, password);
    this.user = user;
  }

  async setup(username: string, password: string, displayName: string) {
    const { user } = await api.setup(username, password, displayName);
    this.user = user;
    if (this.health) this.health.needs_setup = false;
  }

  async logout() {
    await api.logout();
    this.user = null;
  }
}

export const session = new Session();
