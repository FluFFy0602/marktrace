// Command mock is the stand-in for the "Честный знак" (ЦРПТ) code verification
// service: POST /cz/verify (SPEC §8).
//
// Environment:
//
//	ADDR           listen address (default ":8080")
//	REGISTRY_FILE  CSV with header "code" (default "/data/mock_registry.csv")
//	MOCK_MODE      normal | slow | error | garbage (default "normal")
//	MOCK_SLOW_MS   delay in slow mode, ms (default 5000)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// Config is the runtime configuration read from the environment.
type Config struct {
	Addr         string
	RegistryFile string
	Mode         string
	Slow         time.Duration
}

// LoadConfig reads configuration using getenv (os.Getenv in production).
func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	cfg := Config{
		Addr:         get("ADDR", ":8080"),
		RegistryFile: get("REGISTRY_FILE", "/data/mock_registry.csv"),
		Mode:         get("MOCK_MODE", ModeNormal),
	}
	if !validModes[cfg.Mode] {
		return cfg, fmt.Errorf("MOCK_MODE=%q: want normal, slow, error or garbage", cfg.Mode)
	}
	ms, err := strconv.Atoi(get("MOCK_SLOW_MS", "5000"))
	if err != nil || ms < 0 {
		return cfg, fmt.Errorf("MOCK_SLOW_MS=%q: want a non-negative integer", getenv("MOCK_SLOW_MS"))
	}
	cfg.Slow = time.Duration(ms) * time.Millisecond
	return cfg, nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("mock stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		return err
	}
	reg, err := LoadRegistry(cfg.RegistryFile)
	if err != nil {
		return fmt.Errorf("load registry %s: %w", cfg.RegistryFile, err)
	}
	srv, err := NewServer(reg, cfg.Mode, cfg.Slow, log)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	log.Info("mock started", slog.String("addr", cfg.Addr), slog.String("mode", cfg.Mode),
		slog.Int("registry_size", len(reg)), slog.Int64("slow_ms", cfg.Slow.Milliseconds()))

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log.Info("mock shutting down")
	return httpSrv.Shutdown(shutdownCtx)
}
