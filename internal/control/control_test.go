// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package control_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/signalroute/modem-emu/internal/config"
	"github.com/signalroute/modem-emu/internal/control"
	"github.com/signalroute/modem-emu/internal/mux"
)

// ── Test helpers ──────────────────────────────────────────────────────────

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// makeModemConfig creates a minimal modem config with a valid 20-digit ICCID.
func makeModemConfig(i int) config.ModemConfig {
	return config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         fmt.Sprintf("8949020000123456%04d", i),
		IMSI:          fmt.Sprintf("262019000000%03d", i),
		Operator:      "Telekom.de",
		SignalCSQ:     18,
		RegStat:       1,
		SMSStorageMax: 10,
	}
}

// newTestSetup creates a fresh pool with n modems and returns an httptest.Server
// backed by a new control.Server. Using TCPBasePort=0 lets the OS assign ports.
func newTestSetup(t *testing.T, n int) (ts *httptest.Server, iccid string) {
	t.Helper()
	modems := make([]config.ModemConfig, n)
	for i := range modems {
		modems[i] = makeModemConfig(i)
	}
	cfg := &config.EmuConfig{
		Control:   config.ControlConfig{Addr: "127.0.0.1:0"},
		Transport: config.TransportMode{Kind: "tcp", TCPBindAddr: "127.0.0.1", TCPBasePort: 0},
		Modems:    modems,
	}
	pool, err := mux.New(cfg, testLogger())
	if err != nil {
		t.Fatalf("mux.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if n > 0 {
		iccid = pool.Slots()[0].Modem.ICCID()
	}

	srv := control.NewServer(pool, testLogger())
	ts = httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return
}

func get(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func post(t *testing.T, ts *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := ts.Client().Post(ts.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func put(t *testing.T, ts *httptest.Server, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build PUT %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

func del(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build DELETE %s: %v", path, err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := readBody(t, resp)
		t.Errorf("status: got %d, want %d; body=%s", resp.StatusCode, want, body)
	}
}

// ── GET /health ───────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	ts, _ := newTestSetup(t, 1)
	resp := get(t, ts, "/health")
	assertStatus(t, resp, 200)

	var m map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if m["status"] != "ok" {
		t.Errorf("status field: got %q, want ok", m["status"])
	}
}

// ── GET /modems ───────────────────────────────────────────────────────────

func TestListModems(t *testing.T) {
	ts, _ := newTestSetup(t, 1)
	resp := get(t, ts, "/modems")
	assertStatus(t, resp, 200)

	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(arr) != 1 {
		t.Errorf("modem count: got %d, want 1", len(arr))
	}
}

// ── GET /modems/{iccid} ───────────────────────────────────────────────────

func TestGetModem_Found(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := get(t, ts, "/modems/"+iccid+"/")
	assertStatus(t, resp, 200)

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if m["iccid"] != iccid {
		t.Errorf("iccid: got %v, want %q", m["iccid"], iccid)
	}
}

func TestGetModem_NotFound(t *testing.T) {
	ts, _ := newTestSetup(t, 1)
	resp := get(t, ts, "/modems/UNKNOWNICCID00000000/")
	assertStatus(t, resp, 404)
	resp.Body.Close()
}

// ── POST /modems/{iccid}/sms/inject ──────────────────────────────────────

func TestInjectSMS_OK(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	body := `{"from":"+49151","body":"Hello World"}`
	resp := post(t, ts, "/modems/"+iccid+"/sms/inject", body)
	assertStatus(t, resp, 201)
	resp.Body.Close()
}

func TestInjectSMS_MissingFields(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := post(t, ts, "/modems/"+iccid+"/sms/inject", `{"from":""}`)
	assertStatus(t, resp, 400)
	resp.Body.Close()
}

func TestInjectSMS_NotFound(t *testing.T) {
	ts, _ := newTestSetup(t, 1)
	resp := post(t, ts, "/modems/UNKNOWNICCID00000000/sms/inject", `{"from":"+49","body":"x"}`)
	assertStatus(t, resp, 404)
	resp.Body.Close()
}

// ── GET /modems/{iccid}/sms/sent ─────────────────────────────────────────

func TestListSentSMS_Empty(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := get(t, ts, "/modems/"+iccid+"/sms/sent")
	assertStatus(t, resp, 200)

	var arr []any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(arr) != 0 {
		t.Errorf("sent SMS: got %d, want 0", len(arr))
	}
}

// ── DELETE /modems/{iccid}/sms/sent ──────────────────────────────────────

func TestClearSentSMS(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := del(t, ts, "/modems/"+iccid+"/sms/sent")
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

// ── PUT /modems/{iccid}/signal ────────────────────────────────────────────

func TestSetSignal_OK(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := put(t, ts, "/modems/"+iccid+"/signal", `{"csq":15}`)
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

func TestSetSignal_InvalidCSQ(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := put(t, ts, "/modems/"+iccid+"/signal", `{"csq":50}`)
	assertStatus(t, resp, 400)
	resp.Body.Close()
}

func TestSetSignal_CSQ99(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := put(t, ts, "/modems/"+iccid+"/signal", `{"csq":99}`)
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

// ── PUT /modems/{iccid}/registration ─────────────────────────────────────

func TestSetRegistration_OK(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := put(t, ts, "/modems/"+iccid+"/registration", `{"stat":1}`)
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

func TestSetRegistration_InvalidStat(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := put(t, ts, "/modems/"+iccid+"/registration", `{"stat":99}`)
	assertStatus(t, resp, 400)
	resp.Body.Close()
}

// ── GET /modems/{iccid}/storage ───────────────────────────────────────────

func TestGetStorage(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := get(t, ts, "/modems/"+iccid+"/storage")
	assertStatus(t, resp, 200)

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if _, ok := m["used"]; !ok {
		t.Error("response missing 'used' field")
	}
	if _, ok := m["total"]; !ok {
		t.Error("response missing 'total' field")
	}
}

// ── POST /scenarios/ban ───────────────────────────────────────────────────

func TestScenarioBan_WithICCID(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := post(t, ts, "/scenarios/ban?iccid="+iccid, "")
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

func TestScenarioBan_FirstModem(t *testing.T) {
	ts, _ := newTestSetup(t, 1)
	resp := post(t, ts, "/scenarios/ban", "")
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

func TestScenarioBan_EmptyPool(t *testing.T) {
	ts, _ := newTestSetup(t, 0)
	resp := post(t, ts, "/scenarios/ban", "")
	assertStatus(t, resp, 404)
	resp.Body.Close()
}

// ── POST /scenarios/restore ───────────────────────────────────────────────

func TestScenarioRestore(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := post(t, ts, "/scenarios/restore?iccid="+iccid, "")
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

// ── POST /scenarios/weak-signal ───────────────────────────────────────────

func TestScenarioWeakSignal(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := post(t, ts, "/scenarios/weak-signal?iccid="+iccid, "")
	assertStatus(t, resp, 200)
	resp.Body.Close()
}

// ── POST /scenarios/flood ─────────────────────────────────────────────────

func TestScenarioFlood(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := post(t, ts, "/scenarios/flood?iccid="+iccid+"&count=3&from=+49151", "")
	assertStatus(t, resp, 201)

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	injected, _ := m["injected"].(float64)
	if int(injected) != 3 {
		t.Errorf("injected: got %v, want 3", m["injected"])
	}
}

// ── POST /scenarios/fill-storage ─────────────────────────────────────────

func TestScenarioFillStorage(t *testing.T) {
	ts, iccid := newTestSetup(t, 1)
	resp := post(t, ts, "/scenarios/fill-storage?iccid="+iccid, "")
	assertStatus(t, resp, 201)

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if _, ok := m["injected"]; !ok {
		t.Error("response missing 'injected' field")
	}
}
