// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package control_test

// Tests for POST /modems/{iccid}/inject — the +CMT URC injection endpoint.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/signalroute/modem-emu/internal/config"
	"github.com/signalroute/modem-emu/internal/control"
	"github.com/signalroute/modem-emu/internal/mux"
)

// newTestSetupRunning creates a pool (with modem listeners running) plus an
// httptest.Server for the control API. Unlike newTestSetup, it calls pool.Run
// so the modem TCP listeners actually accept connections.
func newTestSetupRunning(t *testing.T) (ts *httptest.Server, iccid string) {
	t.Helper()
	mc := config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         fmt.Sprintf("8949020000123456%04d", 9),
		IMSI:          "262019000000009",
		Operator:      "Telekom.de",
		SignalCSQ:     18,
		RegStat:       1,
		SMSStorageMax: 10,
	}
	cfg := &config.EmuConfig{
		Control:   config.ControlConfig{Addr: "127.0.0.1:0"},
		Transport: config.TransportMode{Kind: "tcp", TCPBindAddr: "127.0.0.1", TCPBasePort: 0},
		Modems:    []config.ModemConfig{mc},
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool, err := mux.New(cfg, log)
	if err != nil {
		t.Fatalf("mux.New: %v", err)
	}
	iccid = pool.Slots()[0].Modem.ICCID()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		pool.Close()
	})
	go pool.Run(ctx)

	srv := control.NewServer(pool, log)
	ts = httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return
}

// TestInjectCMT_Returns201 checks that a valid POST /modems/{iccid}/inject
// returns HTTP 201 with a JSON body confirming the URC was queued.
func TestInjectCMT_Returns201(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)

	resp := post(t, ts, "/modems/"+iccid+"/inject",
		`{"from":"+491234567890","body":"Hello from inject"}`)
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}
	var m map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(m["message"], "+CMT") {
		t.Errorf("expected +CMT in message field, got %q", m["message"])
	}
}

// TestInjectCMT_MissingFields checks that omitting from/body returns 400.
func TestInjectCMT_MissingFields(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)

	cases := []string{
		`{"from":"","body":"Hello"}`,
		`{"from":"+491234","body":""}`,
		`{}`,
	}
	for _, body := range cases {
		resp := post(t, ts, "/modems/"+iccid+"/inject", body)
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("body=%q: expected 400, got %d", body, resp.StatusCode)
		}
	}
}

// TestInjectCMT_UnknownModem checks that injecting to a non-existent ICCID returns 404.
func TestInjectCMT_UnknownModem(t *testing.T) {
	ts, _ := newTestSetup(t, 1)

	resp := post(t, ts, "/modems/00000000000000000000/inject",
		`{"from":"+491234","body":"test"}`)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestInjectCMT_URCDeliveredToATClient verifies that the +CMT URC is actually
// received by a connected AT client after a successful inject call.
func TestInjectCMT_URCDeliveredToATClient(t *testing.T) {
	ts, iccid := newTestSetupRunning(t)

	// Resolve the modem TCP address from /modems list.
	addr := resolveModemAddr(t, ts, iccid)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial modem at %s: %v", addr, err)
	}
	defer conn.Close()

	// Use a single reader for the entire connection to avoid buffer loss.
	rd := bufio.NewReader(conn)
	readLine := func() string {
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		line, _ := rd.ReadString('\n')
		return strings.TrimRight(line, "\r\n")
	}

	// Disable echo and wait for OK.
	conn.Write([]byte("ATE0\r\n"))
	for i := 0; i < 10; i++ {
		if l := readLine(); l == "OK" {
			break
		}
	}

	// Inject via HTTP.
	resp := post(t, ts, "/modems/"+iccid+"/inject",
		`{"from":"+4915198765432","body":"CMT test message"}`)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("inject returned %d", resp.StatusCode)
	}

	// Wait for the URC on the AT socket (two lines: +CMT header then body).
	foundHeader := false
	foundBody := false
	for i := 0; i < 20; i++ {
		line := readLine()
		if strings.Contains(line, "+CMT:") {
			foundHeader = true
		}
		if foundHeader && strings.Contains(line, "CMT test message") {
			foundBody = true
			break
		}
	}
	if !foundHeader || !foundBody {
		t.Errorf("did not receive expected +CMT URC (header=%v body=%v)", foundHeader, foundBody)
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

// resolveModemAddr looks up the TCP addr of the modem by ICCID via GET /modems.
func resolveModemAddr(t *testing.T, ts *httptest.Server, iccid string) string {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + "/modems")
	if err != nil {
		t.Fatalf("GET /modems: %v", err)
	}
	defer resp.Body.Close()
	var modems []struct {
		ICCID string `json:"iccid"`
		Addr  string `json:"addr"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modems); err != nil {
		t.Fatalf("decode /modems: %v", err)
	}
	for _, m := range modems {
		if m.ICCID == iccid {
			return m.Addr
		}
	}
	t.Fatalf("modem %s not found in /modems response", iccid)
	return ""
}
