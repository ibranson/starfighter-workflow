// Package version holds the build version string.
//
// Current is compared against the persisted "daemon_version_last_seen" node
// state on every boot; a change triggers a one-time session wipe so operators
// re-authenticate against the new code after a deploy. Bump it on each deploy
// (the deploy script can also stamp it via -ldflags).
package version

// Current is the running daemon version. Overridable at build time with
//   -ldflags "-X starfighter-workflow/internal/version.Current=<v>"
var Current = "0.1.0-dev"
