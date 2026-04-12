// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Command emu is the go-modem-emu cellular modem emulator.
// It listens on TCP or Unix sockets, one per simulated modem, and speaks
// the AT command protocol to any connecting client (go-sms-gate).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/signalroute/modem-emu/internal/config"
	"github.com/signalroute/modem-emu/internal/control"
	"github.com/signalroute/modem-emu/internal/mux"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath     := flag.String("config", "config.yaml", "path to config.yaml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-modem-emu %s\n", version)
		return nil
	}

	log := buildLogger()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log.Info("go-modem-emu starting",
		"version", version,
		"transport", cfg.Transport.Kind,
		"modems", len(cfg.Modems),
		"control", cfg.Control.Addr,
	)

	pool, err := mux.New(cfg, log)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}
	defer pool.Close()

	// Print the go-sms-gate config snippet.
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════════")
	fmt.Println(" Paste into go-sms-gate config.yaml:")
	fmt.Println("══════════════════════════════════════════════════════════════════")
	fmt.Print(cfg.GatewayConfigHint())
	fmt.Println("══════════════════════════════════════════════════════════════════")
	fmt.Printf("\n Control API: http://%s\n\n", cfg.Control.Addr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Control API.
	ctrlSrv := &http.Server{
		Addr:         cfg.Control.Addr,
		Handler:      control.NewServer(pool, log).Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		if err := ctrlSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("control server", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ctrlSrv.Shutdown(shutCtx)
	}()

	pool.Run(ctx)
	log.Info("shutdown complete")
	return nil
}

// buildLogger constructs a *slog.Logger respecting two env vars:
//
//	LOG_LEVEL  — debug | info | warn | error  (default: info)
//	LOG_FORMAT — json | text                  (default: text)
func buildLogger() *slog.Logger {
	level := slog.LevelInfo
	if s := os.Getenv("LOG_LEVEL"); s != "" {
		var l slog.Level
		if err := l.UnmarshalText([]byte(strings.ToUpper(s))); err == nil {
			level = l
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(h)
}
