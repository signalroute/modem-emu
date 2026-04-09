// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package mux

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/signalroute/go-modem-emu/internal/config"
)

// ── Helpers ───────────────────────────────────────────────────────────────

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func makeTestCfg(n int, kind string) *config.EmuConfig {
	modems := make([]config.ModemConfig, n)
	for i := range modems {
		modems[i] = config.ModemConfig{
			Profile:       "SIM800L",
			ICCID:         fmt.Sprintf("8949020000123456%04d", i),
			IMSI:          fmt.Sprintf("26201900000%04d", i),
			Operator:      "Telekom.de",
			SignalCSQ:     18,
			RegStat:       1,
			SMSStorageMax: 10,
		}
	}
	cfg := &config.EmuConfig{
		Control:   config.ControlConfig{Addr: "127.0.0.1:0"},
		Transport: config.TransportMode{Kind: kind},
		Modems:    modems,
	}
	if kind == "unix" {
		cfg.Transport.UnixBasePath = fmt.Sprintf("/tmp/mux-test-%d", os.Getpid())
	}
	if kind == "tcp" {
		cfg.Transport.TCPBindAddr = "127.0.0.1"
		cfg.Transport.TCPBasePort = 19800 + os.Getpid()%1000
	}
	return cfg
}

// dial opens a connection to modem slot i and sends AT, returns OK/ERROR.
func dialAndPing(t *testing.T, slot *ModemSlot) string {
	t.Helper()
	conn, err := net.DialTimeout(slot.Network, slot.Addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s %s: %v", slot.Network, slot.Addr, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	conn.Write([]byte("ATE0\r\nAT\r\n"))
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "OK" || line == "ERROR" {
			return line
		}
	}
	return "no-response"
}

// ── TCP transport tests ────────────────────────────────────────────────────

func TestPool_TCP_SingleModem(t *testing.T) {
	cfg := makeTestCfg(1, "tcp")
	pool, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go pool.Run(ctx)
	t.Cleanup(func() { cancel(); pool.Close() })

	time.Sleep(30 * time.Millisecond)

	slots := pool.Slots()
	if len(slots) != 1 {
		t.Fatalf("slots: got %d, want 1", len(slots))
	}
	if got := dialAndPing(t, slots[0]); got != "OK" {
		t.Errorf("ping: got %q, want OK", got)
	}
}

func TestPool_TCP_MultipleModems(t *testing.T) {
	const n = 5
	cfg := makeTestCfg(n, "tcp")
	// Offset port to avoid collision.
	cfg.Transport.TCPBasePort += 50

	pool, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go pool.Run(ctx)
	t.Cleanup(func() { cancel(); pool.Close() })

	time.Sleep(30 * time.Millisecond)

	slots := pool.Slots()
	if len(slots) != n {
		t.Fatalf("slots: got %d, want %d", len(slots), n)
	}
	for i, slot := range slots {
		if got := dialAndPing(t, slot); got != "OK" {
			t.Errorf("modem[%d] ping: got %q", i, got)
		}
	}
}

func TestPool_TCP_ICCIDsAreIsolated(t *testing.T) {
	cfg := makeTestCfg(2, "tcp")
	cfg.Transport.TCPBasePort += 100

	pool, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go pool.Run(ctx)
	t.Cleanup(func() { cancel(); pool.Close() })

	time.Sleep(30 * time.Millisecond)

	// Each slot should respond with its own ICCID.
	// Commands are sent one at a time (request/response), matching real gateway
	// behaviour. Sending both in a single write causes bufio.Scanner on the
	// modem side to issue an extra Read() after the nil-token for "\n", which
	// on macOS loopback returns EOF when the peer's send buffer is exhausted.
	for i, slot := range pool.Slots() {
		expectedICCID := fmt.Sprintf("8949020000123456%04d", i)

		conn, err := net.DialTimeout(slot.Network, slot.Addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial modem[%d]: %v", i, err)
		}
		conn.SetDeadline(time.Now().Add(4 * time.Second))

		sc := bufio.NewScanner(conn)

		// Step 1: disable echo.
		conn.Write([]byte("ATE0\r\n"))
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "OK" {
				break
			}
		}

		// Step 2: query ICCID.
		conn.Write([]byte("AT+CCID?\r\n"))
		found := false
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.Contains(line, expectedICCID) {
				found = true
			}
			if found && line == "OK" {
				break
			}
		}
		conn.Close()

		if !found {
			t.Errorf("modem[%d]: ICCID %q not found in response", i, expectedICCID)
		}
	}
}

// ── Unix socket tests ──────────────────────────────────────────────────────

func TestPool_Unix_SingleModem(t *testing.T) {
	cfg := makeTestCfg(1, "unix")
	pool, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go pool.Run(ctx)
	t.Cleanup(func() { cancel(); pool.Close() })

	time.Sleep(30 * time.Millisecond)

	slots := pool.Slots()
	if len(slots) != 1 {
		t.Fatalf("slots: %d", len(slots))
	}
	if got := dialAndPing(t, slots[0]); got != "OK" {
		t.Errorf("ping: %q", got)
	}
}

func TestPool_Unix_SocketCleanupOnStop(t *testing.T) {
	cfg := makeTestCfg(1, "unix")
	cfg.Transport.UnixBasePath = fmt.Sprintf("/tmp/mux-cleanup-%d", os.Getpid())

	pool, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sockPath := cfg.AddrForModem(0)

	// Socket file must exist after New.
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Error("socket file not created")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pool.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("pool did not stop within 2s")
	}

	// Socket file must be removed after shutdown.
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket file not cleaned up after shutdown")
	}
}

// ── Lookup ────────────────────────────────────────────────────────────────

func TestPool_Lookup_ByICCID(t *testing.T) {
	cfg := makeTestCfg(3, "tcp")
	cfg.Transport.TCPBasePort += 200

	pool, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Close()

	for i := 0; i < 3; i++ {
		iccid := fmt.Sprintf("8949020000123456%04d", i)
		slot, ok := pool.Lookup(iccid)
		if !ok {
			t.Errorf("Lookup(%q): not found", iccid)
			continue
		}
		if slot.Modem.ICCID() != iccid {
			t.Errorf("Lookup(%q): got ICCID %q", iccid, slot.Modem.ICCID())
		}
	}

	_, ok := pool.Lookup("NONEXISTENT")
	if ok {
		t.Error("Lookup(NONEXISTENT) should return false")
	}
}

// ── SMS injection through pool ────────────────────────────────────────────

func TestPool_InjectSMS_ReachesClient(t *testing.T) {
	cfg := makeTestCfg(1, "tcp")
	cfg.Transport.TCPBasePort += 300

	pool, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go pool.Run(ctx)
	t.Cleanup(func() { cancel(); pool.Close() })

	time.Sleep(30 * time.Millisecond)

	slot := pool.Slots()[0]

	// Connect a "gateway" client.
	conn, err := net.DialTimeout(slot.Network, slot.Addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Disable echo so URCs are unambiguous.
	conn.Write([]byte("ATE0\r\n"))
	// Drain the ATE0 OK.
	time.Sleep(20 * time.Millisecond)

	// Capture lines in background.
	linesCh := make(chan string, 32)
	go func() {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			linesCh <- strings.TrimSpace(sc.Text())
		}
		close(linesCh)
	}()

	// Give the goroutine a moment to start reading.
	time.Sleep(20 * time.Millisecond)

	// Inject an SMS.
	slot.Modem.InjectSMS("+4915198765432", "Your OTP is 391827")

	// Expect +CMTI URC.
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case line := <-linesCh:
			if strings.HasPrefix(line, "+CMTI:") {
				return // 
			}
		case <-deadline:
			t.Error("timed out waiting for +CMTI URC")
			return
		}
	}
}
