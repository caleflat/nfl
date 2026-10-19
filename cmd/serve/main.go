package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caleflat/nfl/internal/api"
	"github.com/caleflat/nfl/internal/database"
	"github.com/caleflat/nfl/internal/plays"
	"github.com/caleflat/nfl/internal/summary"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	queryTimeout := flag.Duration("query-timeout", 5*time.Second, "summary database query deadline")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, *addr, *queryTimeout); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, addr string, queryTimeout time.Duration) error {
	if queryTimeout <= 0 || queryTimeout > 15*time.Second {
		return errors.New("-query-timeout must be positive and at most 15s")
	}
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	server := &http.Server{
		Addr: addr, Handler: api.NewHandler(summary.Store{DB: db}, plays.Store{DB: db}, logger, queryTimeout),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
		BaseContext:    func(_ net.Listener) context.Context { return ctx },
	}
	stopped := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", addr)
		stopped <- server.ListenAndServe()
	}()
	select {
	case err := <-stopped:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
			return err
		}
		return nil
	}
}
