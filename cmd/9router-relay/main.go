package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hoangdodev/9router-relay/internal/relay"
)

var version = "dev"

type config struct {
	listenAddr   string
	upstreamAddr string
	dialTimeout  time.Duration
	verbose      bool
	showVersion  bool
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func parseFlags(args []string) (*config, error) {
	fs := flag.NewFlagSet("9router-relay", flag.ContinueOnError)
	cfg := &config{}
	fs.StringVar(&cfg.listenAddr, "listen", envOrDefault("ROUTER_RELAY_LISTEN_ADDR", "127.0.0.1:20129"), "local TCP listen address")
	fs.StringVar(&cfg.upstreamAddr, "upstream", envOrDefault("ROUTER_RELAY_UPSTREAM_ADDR", "161.248.239.201:20128"), "upstream TCP address")
	fs.DurationVar(&cfg.dialTimeout, "dial-timeout", 10*time.Second, "upstream connection timeout")
	fs.BoolVar(&cfg.verbose, "v", false, "enable debug logging")
	fs.BoolVar(&cfg.showVersion, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return cfg, nil
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	if cfg.showVersion {
		fmt.Println(version)
		return
	}

	level := slog.LevelInfo
	if cfg.verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	server, err := relay.NewServer(relay.Config{
		ListenAddr:   cfg.listenAddr,
		UpstreamAddr: cfg.upstreamAddr,
		DialTimeout:  cfg.dialTimeout,
	}, logger)
	if err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("starting TCP relay", "listen", cfg.listenAddr, "upstream", cfg.upstreamAddr)
	if err := server.Serve(ctx); err != nil && err != relay.ErrServerClosed {
		logger.Error("relay stopped with error", "err", err)
		os.Exit(1)
	}
	logger.Info("relay stopped")
}
