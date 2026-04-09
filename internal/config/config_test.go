// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package config

import (
	"fmt"
	"os"
	"strings"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(content), 0600)
	return p
}

func TestLoad_Minimal(t *testing.T) {
	p := writeJSON(t, `{"modems":[{"iccid":"89490200001234567890"}]}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Control.Addr != "127.0.0.1:8888" {
		t.Errorf("control default: %q", cfg.Control.Addr)
	}
	if cfg.Transport.Kind != "unix" {
		t.Errorf("transport default: %q", cfg.Transport.Kind)
	}
	if cfg.Modems[0].Profile != "SIM800L" {
		t.Errorf("profile default: %q", cfg.Modems[0].Profile)
	}
}

func TestLoad_Defaults(t *testing.T) {
	p := writeJSON(t, `{"modems":[{"iccid":"89490200001234567890"}]}`)
	cfg, _ := Load(p)

	checks := []struct{ name string; got, want any }{
		{"control.addr", cfg.Control.Addr, "127.0.0.1:8888"},
		{"transport.kind", cfg.Transport.Kind, "unix"},
		{"transport.unix_base_path", cfg.Transport.UnixBasePath, "/tmp/modem-emu"},
		{"transport.tcp_base_port", cfg.Transport.TCPBasePort, 7000},
		{"transport.tcp_bind_addr", cfg.Transport.TCPBindAddr, "127.0.0.1"},
		{"modem.profile", cfg.Modems[0].Profile, "SIM800L"},
		{"modem.signal_csq", cfg.Modems[0].SignalCSQ, 18},
		{"modem.reg_stat", cfg.Modems[0].RegStat, 1},
		{"modem.sms_storage_max", cfg.Modems[0].SMSStorageMax, 30},
		{"modem.response_delay_ms", cfg.Modems[0].ResponseDelayMs, 5},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_ExplicitValues(t *testing.T) {
	p := writeJSON(t, `{
		"control": {"addr": "0.0.0.0:9999"},
		"transport": {"kind": "tcp", "tcp_base_port": 8000, "tcp_bind_addr": "0.0.0.0"},
		"modems": [
			{
				"iccid": "89490200001234567890",
				"profile": "EC21",
				"signal_csq": 25,
				"reg_stat": 5,
				"sms_storage_max": 50,
				"response_delay_ms": 20
			}
		]
	}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Control.Addr != "0.0.0.0:9999"    { t.Errorf("control.addr: %q", cfg.Control.Addr) }
	if cfg.Transport.Kind != "tcp"            { t.Errorf("transport.kind: %q", cfg.Transport.Kind) }
	if cfg.Transport.TCPBasePort != 8000      { t.Errorf("tcp_base_port: %d", cfg.Transport.TCPBasePort) }
	if cfg.Modems[0].Profile != "EC21"        { t.Errorf("profile: %q", cfg.Modems[0].Profile) }
	if cfg.Modems[0].SignalCSQ != 25          { t.Errorf("signal_csq: %d", cfg.Modems[0].SignalCSQ) }
	if cfg.Modems[0].RegStat != 5             { t.Errorf("reg_stat: %d", cfg.Modems[0].RegStat) }
	if cfg.Modems[0].SMSStorageMax != 50      { t.Errorf("sms_storage_max: %d", cfg.Modems[0].SMSStorageMax) }
	if cfg.Modems[0].ResponseDelayMs != 20    { t.Errorf("response_delay_ms: %d", cfg.Modems[0].ResponseDelayMs) }
}

func TestLoad_Validation_NoModems(t *testing.T) {
	p := writeJSON(t, `{"modems":[]}`)
	_, err := Load(p)
	if err == nil {
		t.Error("expected error for empty modems list")
	}
}

func TestLoad_Validation_BadICCID(t *testing.T) {
	p := writeJSON(t, `{"modems":[{"iccid":"tooshort"}]}`)
	_, err := Load(p)
	if err == nil {
		t.Error("expected error for short ICCID")
	}
}

func TestLoad_Validation_BadTransport(t *testing.T) {
	p := writeJSON(t, `{
		"transport": {"kind": "serial"},
		"modems": [{"iccid":"89490200001234567890"}]
	}`)
	_, err := Load(p)
	if err == nil {
		t.Error("expected error for transport.kind=serial")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	p := writeJSON(t, `{modems: [not json`)
	_, err := Load(p)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestAddrForModem_Unix(t *testing.T) {
	cfg := &EmuConfig{
		Transport: TransportMode{Kind: "unix", UnixBasePath: "/tmp/emu"},
	}
	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("/tmp/emu-%d.sock", i)
		if got := cfg.AddrForModem(i); got != want {
			t.Errorf("AddrForModem(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestAddrForModem_TCP(t *testing.T) {
	cfg := &EmuConfig{
		Transport: TransportMode{Kind: "tcp", TCPBindAddr: "127.0.0.1", TCPBasePort: 7000},
	}
	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("127.0.0.1:%d", 7000+i)
		if got := cfg.AddrForModem(i); got != want {
			t.Errorf("AddrForModem(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestGatewayConfigHint_ContainsICCID(t *testing.T) {
	cfg := &EmuConfig{
		Transport: TransportMode{Kind: "unix", UnixBasePath: "/tmp/emu"},
		Modems: []ModemConfig{
			{ICCID: "89490200001234567890", Profile: "SIM800L"},
		},
	}
	hint := cfg.GatewayConfigHint()
	if hint == "" {
		t.Error("hint is empty")
	}
	if !containsStr(hint, "89490200001234567890") {
		t.Error("hint does not contain ICCID")
	}
	if !containsStr(hint, "/tmp/emu-0.sock") {
		t.Error("hint does not contain socket path")
	}
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
