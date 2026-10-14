package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/GerhardOfRivia/weer/internal/config"
	"github.com/GerhardOfRivia/weer/internal/httpapi"
	"github.com/GerhardOfRivia/weer/internal/ingest"
	"github.com/GerhardOfRivia/weer/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: weer serve|ingest|version")
	}
	if os.Args[1] == "version" {
		fmt.Println("weer 0.1.0")
		return nil
	}
	command := os.Args[1]
	if command != "serve" && command != "ingest" {
		return fmt.Errorf("unknown command %q", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	path := fs.String("config", "weer.yaml", "YAML config")
	file := fs.String("file", "", "local Level II archive")
	if e := fs.Parse(os.Args[2:]); e != nil {
		return e
	}
	c, e := config.Load(*path)
	if e != nil {
		return e
	}
	if e = c.Prepare(); e != nil {
		return e
	}
	lock, e := ingest.Lock(c.DataDir)
	if e != nil {
		return e
	}
	defer lock.Close()
	s, e := store.Open(c.Database, c.Stations)
	if e != nil {
		return e
	}
	defer s.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	engine := ingest.New(c, s)
	if e = engine.Recover(); e != nil {
		return e
	}
	if command == "ingest" {
		if *file == "" {
			return fmt.Errorf("ingest requires --file")
		}
		return engine.Import(ctx, *file)
	}
	done := make(chan struct{})
	go func() { defer close(done); engine.Run(ctx) }()
	server := &http.Server{Addr: c.Listen, Handler: httpapi.Handler(c, s), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		server.Shutdown(shutdown)
	}()
	slog.Info("Weer listening", "address", c.Listen)
	e = server.ListenAndServe()
	cancel()
	<-done
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if e := run(); e != nil {
		slog.Error("weer", "error", e)
		os.Exit(1)
	}
}
