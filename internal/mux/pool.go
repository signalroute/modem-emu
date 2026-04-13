// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package mux runs one socket listener per simulated modem.
// Each accepted connection gets its own goroutine running the AT state machine.
// Multiple connections to the same modem socket are serialised (only one
// "virtual serial port" at a time — matching real modem behaviour).
package mux

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/signalroute/modem-emu/internal/config"
	"github.com/signalroute/modem-emu/internal/metrics"
	"github.com/signalroute/modem-emu/internal/modem"
)

// ModemSlot pairs a Modem state machine with its listener address.
type ModemSlot struct {
	Index  int
	Modem  *modem.Modem
	Addr   string // what the gateway dials
	Network string // "unix" or "tcp"
}

// Pool holds all modem slots and their listeners.
type Pool struct {
	slots     []*ModemSlot
	listeners []net.Listener
	cfg       *config.EmuConfig
	log       *slog.Logger
}

// New creates a Pool: allocates Modem instances and opens listeners.
func New(cfg *config.EmuConfig, log *slog.Logger) (*Pool, error) {
	p := &Pool{cfg: cfg, log: log}

	network := cfg.Transport.Kind // "unix" or "tcp"

	for i, mc := range cfg.Modems {
		m := modem.New(mc, log)
		addr := cfg.AddrForModem(i)

		// Remove stale Unix socket files.
		if network == "unix" {
			os.Remove(addr)
		}

		ln, err := net.Listen(network, addr)
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("modem[%d]: listen %s %s: %w", i, network, addr, err)
		}

		// Use the actual bound address (important when port 0 is configured).
		boundAddr := ln.Addr().String()

		p.slots = append(p.slots, &ModemSlot{
			Index:   i,
			Modem:   m,
			Addr:    boundAddr,
			Network: network,
		})
		p.listeners = append(p.listeners, ln)

		log.Info("modem listening",
			"index", i,
			"iccid", mc.ICCID,
			"profile", mc.Profile,
			"network", network,
			"addr", boundAddr,
		)
	}
	return p, nil
}

// Run starts accepting connections on all listeners and blocks until ctx is cancelled.
// Each accepted connection spawns a goroutine running the AT state machine.
// Connections to the same listener are serialised (like a real serial port).
func (p *Pool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i, slot := range p.slots {
		slot := slot
		ln := p.listeners[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.serveSlot(ctx, slot, ln)
		}()
	}
	<-ctx.Done()
	// Close all listeners so Accept() unblocks.
	for _, ln := range p.listeners {
		ln.Close()
	}
	wg.Wait()

	// Clean up Unix socket files.
	if p.cfg.Transport.Kind == "unix" {
		for _, slot := range p.slots {
			os.Remove(slot.Addr)
		}
	}
}

// serveSlot accepts connections on a single listener one at a time.
// Once one connection closes the next is accepted — this serialises access
// to the modem's state, matching real serial port semantics.
func (p *Pool) serveSlot(ctx context.Context, slot *ModemSlot, ln net.Listener) {
	log := p.log.With("iccid", slot.Modem.ICCID(), "addr", slot.Addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Error("accept error", "err", err)
				return
			}
		}
		log.Info("client connected", "remote", conn.RemoteAddr())
		metrics.ActiveConnections.Add(1)
		// RunSession blocks for the lifetime of the connection.
		// The modem state (signal, reg, storage) persists across reconnects —
		// matching gateway restart behaviour.
		slot.Modem.RunSession(ctx, conn)
		metrics.ActiveConnections.Add(-1)
		log.Info("client disconnected")
	}
}

// Slots returns all modem slots (for the control API and status display).
func (p *Pool) Slots() []*ModemSlot { return p.slots }

// Lookup finds a slot by ICCID.
func (p *Pool) Lookup(iccid string) (*ModemSlot, bool) {
	for _, s := range p.slots {
		if s.Modem.ICCID() == iccid {
			return s, true
		}
	}
	return nil, false
}

// Close shuts down all listeners.
func (p *Pool) Close() {
	for _, ln := range p.listeners {
		ln.Close()
	}
}
