// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package control_test

// Regression tests for issue #42: bearer token authentication on the control API.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/signalroute/modem-emu/internal/config"
	"github.com/signalroute/modem-emu/internal/control"
	"github.com/signalroute/modem-emu/internal/mux"
)

// newAuthTestSetup creates a test server with MODEM_EMU_TOKEN set to token.
func newAuthTestSetup(t *testing.T, token string) *httptest.Server {
	t.Helper()

	t.Setenv("MODEM_EMU_TOKEN", token)

	cfg := &config.EmuConfig{
		Control:   config.ControlConfig{Addr: "127.0.0.1:0"},
		Transport: config.TransportMode{Kind: "tcp", TCPBindAddr: "127.0.0.1", TCPBasePort: 0},
		Modems: []config.ModemConfig{
			{
				Profile:       "SIM800L",
				ICCID:         fmt.Sprintf("8949020000123456%04d", 0),
				IMSI:          "262019000000001",
				Operator:      "Telekom.de",
				SignalCSQ:     18,
				RegStat:       1,
				SMSStorageMax: 10,
			},
		},
	}
	pool, err := mux.New(cfg, testLogger())
	if err != nil {
		t.Fatalf("mux.New: %v", err)
	}
	t.Cleanup(pool.Close)

	srv := control.NewServer(pool, testLogger())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func doGet(t *testing.T, ts *httptest.Server, path, authHeader string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", path, err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// TestAuth_NoTokenEnvVar verifies that when MODEM_EMU_TOKEN is empty the
// server runs unauthenticated and every request is allowed.
func TestAuth_NoTokenEnvVar(t *testing.T) {
	t.Setenv("MODEM_EMU_TOKEN", "")

	cfg := &config.EmuConfig{
		Control:   config.ControlConfig{Addr: "127.0.0.1:0"},
		Transport: config.TransportMode{Kind: "tcp", TCPBindAddr: "127.0.0.1", TCPBasePort: 0},
		Modems: []config.ModemConfig{makeModemConfig(0)},
	}
	pool, err := mux.New(cfg, testLogger())
	if err != nil {
		t.Fatalf("mux.New: %v", err)
	}
	t.Cleanup(pool.Close)

	ts := httptest.NewServer(control.NewServer(pool, testLogger()).Handler())
	t.Cleanup(ts.Close)

	// No Authorization header → should succeed (200).
	resp := doGet(t, ts, "/health", "")
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

// TestAuth_ValidToken verifies that a correct Bearer token grants access.
func TestAuth_ValidToken(t *testing.T) {
	const secret = "supersecret123"
	ts := newAuthTestSetup(t, secret)

	resp := doGet(t, ts, "/health", "Bearer "+secret)
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

// TestAuth_MissingToken verifies that a request without an Authorization header
// is rejected with 401 when a token is configured.
func TestAuth_MissingToken(t *testing.T) {
	ts := newAuthTestSetup(t, "mysecret")

	resp := doGet(t, ts, "/health", "")
	assertStatus(t, resp, 401)
	resp.Body.Close()
}

// TestAuth_WrongToken verifies that a wrong Bearer token is rejected with 401.
func TestAuth_WrongToken(t *testing.T) {
	ts := newAuthTestSetup(t, "correcttoken")

	resp := doGet(t, ts, "/health", "Bearer wrongtoken")
	assertStatus(t, resp, 401)
	resp.Body.Close()
}

// TestAuth_WrongScheme verifies that a non-Bearer auth scheme is rejected.
func TestAuth_WrongScheme(t *testing.T) {
	ts := newAuthTestSetup(t, "correcttoken")

	resp := doGet(t, ts, "/health", "Basic correcttoken")
	assertStatus(t, resp, 401)
	resp.Body.Close()
}

// TestAuth_TokenProtectsAllEndpoints verifies that auth is applied globally,
// not only on /health.
func TestAuth_TokenProtectsAllEndpoints(t *testing.T) {
	const secret = "globaltoken"
	ts := newAuthTestSetup(t, secret)

	paths := []string{
		"/modems",
		"/health",
		"/modems/8949020000123456" + fmt.Sprintf("%04d", 0),
	}
	for _, p := range paths {
		resp := doGet(t, ts, p, "") // no token
		assertStatus(t, resp, 401)
		resp.Body.Close()

		resp = doGet(t, ts, p, "Bearer "+secret) // valid token
		if resp.StatusCode == 401 {
			t.Errorf("GET %s with valid token: got 401", p)
		}
		resp.Body.Close()
	}
}
