# starfighter-workflow

A headless web app for tracking inbound **arcade-game repair requests**, designed
to run on a Raspberry Pi 5 — including *alongside* the existing starfighters A/V
controller on the same Pi, without interfering with it.

It is a single statically-linked Go binary (`sfworkflowd`) that serves a
SvelteKit single-page app plus a JSON API over HTTP, persisting everything to a
local SQLite file. No console, no HDMI, no external processes — so it shares no
surface with the A/V daemon and is trivially portable to a Pi of its own.

## Architecture at a glance

| Concern | Choice | Why |
|---|---|---|
| Language / binary | Go, one static binary, `CGO_ENABLED=0` | Cross-compiles Windows → linux/arm64 with no C toolchain |
| Storage | SQLite via `modernc.org/sqlite` (pure Go) | No CGo; WAL mode; embedded migrations |
| Frontend | SvelteKit + `adapter-static`, embedded via `embed.FS` | One binary ships the whole app |
| Auth | Cookie session + bcrypt, sliding 12h TTL | Same token doubles as a bearer for future API clients |
| Roles | `admin`, `user` | A `user` does everything; `admin` *also* manages users |
| Listen port | **`:9090`** | Stays clear of the A/V daemon's `:8080` on a shared Pi |
| Service user | `DynamicUser` + `StateDirectory` | Unprivileged; isolated state; no shared files with the A/V app |

### Coexistence with the A/V app (sibling project `starfighters2`)
- **Different port** (`:9090` vs `:8080`), **different data dir**
  (`/var/lib/sfworkflowd`), **different config** (`/etc/sfworkflowd/config.json`),
  **different systemd unit** (`sfworkflowd.service`).
- Runs as an unprivileged `DynamicUser` — it never touches `tty1`, HDMI, CEC,
  or any device the A/V daemon owns.
- No mDNS cluster / peer sync — this is a single instance.

## Layout

```
cmd/sfworkflowd/        daemon entrypoint
internal/
  auth/                 users, bcrypt, sessions (roles: admin, user)
  config/               JSON config load + atomic update
  db/                   SQLite open + embedded migrations
    migrations/         0001_init.sql (users, sessions, repair_requests, request_events)
  nodestate/            tiny key/value store (version bookkeeping)
  repair/               repair-request domain + workflow STATE MACHINE (states.go)
  httpapi/              HTTP routes, middleware, handlers
  web/                  embeds the built SPA (dist/)
  version/              build version string
web/                    SvelteKit SPA source
deploy/                 systemd unit + Pi setup script
scripts/                PowerShell deploy/provision/logs/restart
```

## The workflow state machine

The repair lifecycle (valid statuses + legal transitions) is **provisional** and
lives entirely in [`internal/repair/states.go`](internal/repair/states.go). The
`status` column is free TEXT — no SQL CHECK pins it down — so the final spec can
land without a schema migration. The current placeholder pipeline is:

```
received → diagnosing → awaiting_parts ⇄ in_repair → testing → ready_for_pickup → completed
            (any active state may go → on_hold, and → cancelled)
```

Every transition, assignment, priority change, and note is appended to
`request_events` for an immutable audit trail (the request's History tab).

## Develop

```
# Backend only, against a throwaway local DB on :9090
make build && ./bin/sfworkflowd-host -config dev-data/config.json   # or: .\sf.ps1 run

# Frontend with hot reload (proxies /api to a running daemon on :9090)
cd web && npm install && npm run dev
```

## Build & deploy to a Pi

```
.\sf.ps1 provision ibranson@workflow.local   # one-time: install + enable the service
.\sf.ps1 deploy    ibranson@workflow.local   # build SPA + arm64 binary, ship, restart, healthz
.\sf.ps1 logs      ibranson@workflow.local
```

Then browse to `http://workflow.local:9090/` and create the first admin.

> First boot shows a setup wizard (because the users table is empty). After the
> first admin exists, `/api/v1/setup` is closed and the login screen takes over.
