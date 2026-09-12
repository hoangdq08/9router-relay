package main

import (
	"testing"
	"time"
)

func TestParseFlagsDefaultsAndOverrides(t *testing.T) {
	t.Setenv("ROUTER_RELAY_LISTEN_ADDR", "127.0.0.1:31001")
	t.Setenv("ROUTER_RELAY_UPSTREAM_ADDR", "127.0.0.1:31002")
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.listenAddr != "127.0.0.1:31001" || cfg.upstreamAddr != "127.0.0.1:31002" {
		t.Fatalf("env defaults were not used: %+v", cfg)
	}

	cfg, err = parseFlags([]string{"-listen", "127.0.0.1:32001", "-upstream", "127.0.0.1:32002", "-dial-timeout", "3s", "-v"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.listenAddr != "127.0.0.1:32001" || cfg.upstreamAddr != "127.0.0.1:32002" || cfg.dialTimeout != 3*time.Second || !cfg.verbose {
		t.Fatalf("flag overrides were not used: %+v", cfg)
	}
}

func TestParseFlagsRejectsInvalidDuration(t *testing.T) {
	if _, err := parseFlags([]string{"-dial-timeout", "invalid"}); err == nil {
		t.Fatal("invalid duration accepted")
	}
}
