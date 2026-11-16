// Command server runs the MarkTrace backend API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"marktrace/backend/internal/httpapi"
	"marktrace/backend/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dbURL := env("DATABASE_URL", "postgres://marktrace:marktrace@db:5432/marktrace?sslmode=disable")
	mockURL := env("MOCK_URL", "http://mock:8080")
	goldenDir := env("GOLDEN_DIR", "/golden")
	addr := env("LISTEN_ADDR", ":8080")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Connect(ctx, dbURL, 2*time.Minute)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	if err := store.Init(ctx, pool, bcrypt.DefaultCost); err != nil {
		log.Fatalf("db init: %v", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(pool, mockURL, goldenDir).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	log.Printf("marktrace backend listening on %s (mock %s, golden %s)", addr, mockURL, goldenDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
