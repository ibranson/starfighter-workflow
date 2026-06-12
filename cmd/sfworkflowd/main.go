// Command sfworkflowd is the arcade-game repair-workflow daemon.
//
// It is a pure, headless network service: it serves an embedded SvelteKit SPA
// plus a JSON API over HTTP and persists everything to a local SQLite file. It
// owns no console, no HDMI, no display, and shells out to nothing — so it runs
// cleanly alongside the starfighters A/V daemon on the same Pi (different port,
// data dir, and config path) and is trivially portable to a Pi of its own.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embed the IANA timezone DB so time.LoadLocation works on a stripped
	// Pi OS Lite image without /usr/share/zoneinfo. ~450KB.
	_ "time/tzdata"

	"starfighter-workflow/internal/auth"
	"starfighter-workflow/internal/config"
	"starfighter-workflow/internal/db"
	"starfighter-workflow/internal/httpapi"
	"starfighter-workflow/internal/nodestate"
	"starfighter-workflow/internal/repair"
	"starfighter-workflow/internal/version"
)

func main() {
	configPath := flag.String("config", "/etc/sfworkflowd/config.json", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		println(version.Current)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}
	logger.Info("sfworkflowd starting", "addr", cfg.HTTP.Addr, "data_dir", cfg.DataDir, "version", version.Current)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d, err := db.Open(ctx, cfg.DataDir, logger)
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	authSvc := auth.NewService(d)
	repairSvc := repair.NewService(d)
	nodeStore := nodestate.New(d)

	// Post-deploy session wipe: if the version string changed since the last
	// successful boot, drop all sessions so everyone re-authenticates against
	// the new code. Incidental restarts (same binary) keep sessions intact.
	const verStateKey = "daemon_version_last_seen"
	if prevVer, ok, err := nodeStore.Get(ctx, verStateKey); err != nil {
		logger.Warn("read daemon_version_last_seen", "err", err)
	} else if ok && prevVer != version.Current {
		if n, err := authSvc.WipeAllSessions(ctx); err != nil {
			logger.Warn("post-deploy session wipe failed", "err", err)
		} else {
			logger.Info("post-deploy session wipe", "prev_version", prevVer, "new_version", version.Current, "wiped", n)
		}
		if err := nodeStore.Set(ctx, verStateKey, version.Current); err != nil {
			logger.Warn("record daemon version failed", "err", err)
		}
	} else if !ok {
		// First boot ever — record the version without wiping.
		if err := nodeStore.Set(ctx, verStateKey, version.Current); err != nil {
			logger.Warn("record daemon version failed", "err", err)
		}
	}

	// Periodically purge expired sessions so the table doesn't grow forever.
	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := authSvc.PurgeExpired(ctx); err != nil {
					logger.Warn("purge expired sessions", "err", err)
				}
			}
		}
	}()

	srv := httpapi.NewServer(cfg, *configPath, logger, d, authSvc, repairSvc)

	httpSrv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal", "sig", sig.String())
	case err := <-errCh:
		logger.Error("http server", "err", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "err", err)
	}
	logger.Info("sfworkflowd stopped")
}
