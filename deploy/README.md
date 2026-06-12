# Deploy

`sfworkflowd` installs as a single systemd service. Everything here is scoped so
it can run on the same Pi as the starfighters A/V daemon without contention.

## Files
- `sfworkflowd.service` — the systemd unit. Runs unprivileged via `DynamicUser`,
  stores state in `/var/lib/sfworkflowd` (`StateDirectory`), reads config from
  `/etc/sfworkflowd/config.json`, logs to the journal, listens on `:9090`.
- `setup-pi.sh` — idempotent installer: drops the unit in place, ensures
  `/etc/sfworkflowd` exists, enables the service. Installs no other packages.

## First-time provisioning (from your Windows dev box)
```
.\scripts\provision.ps1 -RemoteHost ibranson@workflow.local
.\scripts\deploy.ps1    -RemoteHost ibranson@workflow.local
```
`provision` installs + enables the service and writes the config; `deploy`
cross-compiles, ships the binary, restarts, and waits on `/healthz`.

## Config (`/etc/sfworkflowd/config.json`)
```json
{
  "data_dir": "/var/lib/sfworkflowd",
  "display_name": "Repair Workflow",
  "http": { "addr": ":9090" }
}
```
- `data_dir` — SQLite `state.db` lives here. With `DynamicUser`+`StateDirectory`,
  systemd owns and persists it across restarts/reinstalls.
- `http.addr` — keep this distinct from the A/V daemon's `:8080`.

## Coexistence guarantees
The unit deliberately omits everything the A/V appliance unit needs:
no `Conflicts=getty@tty1`, no `TTYPath`, no `StandardOutput=tty`, no device
access. It only opens a TCP socket and writes its own state dir — there is no
path by which it can disturb video, audio, or the console.

## Useful commands on the Pi
```
sudo systemctl status sfworkflowd.service
sudo journalctl -u sfworkflowd.service -f
sudo systemctl restart sfworkflowd.service
```

## Reset state
Stop the service and remove the state dir (this deletes all requests + users):
```
sudo systemctl stop sfworkflowd.service
sudo rm -rf /var/lib/sfworkflowd/*
sudo systemctl start sfworkflowd.service
```
Next boot will show the first-admin setup wizard again.
