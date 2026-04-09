// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

// Package config loads emulator configuration from a JSON file.
// JSON is used directly to avoid proxy-blocked YAML dependencies in CI.
// For production, swap json.Unmarshal for yaml.Unmarshal.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EmuConfig is the top-level emulator configuration.
type EmuConfig struct {
	Control   ControlConfig `json:"control"`
	Transport TransportMode `json:"transport"`
	Modems    []ModemConfig `json:"modems"`
}

// ControlConfig holds the HTTP control/injection API config.
type ControlConfig struct {
	Addr string `json:"addr"`
}

// TransportMode controls how the emulator exposes modem endpoints.
type TransportMode struct {
	// Kind: "unix" or "tcp"
	Kind string `json:"kind"`

	// For unix: base path. Modem N → <base>-N.sock
	UnixBasePath string `json:"unix_base_path"`

	// For tcp: modem N listens on bind_addr:(base_port+N)
	TCPBasePort int    `json:"tcp_base_port"`
	TCPBindAddr string `json:"tcp_bind_addr"`
}

// ModemConfig defines one simulated modem.
type ModemConfig struct {
	Profile         string `json:"profile"`
	ICCID           string `json:"iccid"`
	IMSI            string `json:"imsi"`
	MSISDN          string `json:"msisdn"`
	Operator        string `json:"operator"`
	OperatorMCC     string `json:"operator_mcc"`
	OperatorMNC     string `json:"operator_mnc"`
	SignalCSQ       int    `json:"signal_csq"`
	RegStat         int    `json:"reg_stat"`
	SMSStorageMax   int    `json:"sms_storage_max"`
	ResponseDelayMs int    `json:"response_delay_ms"`
}

func applyDefaults(cfg *EmuConfig) {
	if cfg.Control.Addr == ""           { cfg.Control.Addr = "127.0.0.1:8888" }
	if cfg.Transport.Kind == ""         { cfg.Transport.Kind = "unix" }
	if cfg.Transport.UnixBasePath == "" { cfg.Transport.UnixBasePath = "/tmp/modem-emu" }
	if cfg.Transport.TCPBasePort == 0   { cfg.Transport.TCPBasePort = 7000 }
	if cfg.Transport.TCPBindAddr == ""  { cfg.Transport.TCPBindAddr = "127.0.0.1" }

	for i := range cfg.Modems {
		m := &cfg.Modems[i]
		if m.Profile == ""         { m.Profile = "SIM800L" }
		if m.ICCID == ""           { m.ICCID = fmt.Sprintf("8949020000123456%04d", i) }
		if m.IMSI == ""            { m.IMSI = fmt.Sprintf("26201900000%04d", i) }
		if m.Operator == ""        { m.Operator = "Telekom.de" }
		if m.OperatorMCC == ""     { m.OperatorMCC = "262" }
		if m.OperatorMNC == ""     { m.OperatorMNC = "01" }
		if m.SignalCSQ == 0        { m.SignalCSQ = 18 }
		if m.RegStat == 0          { m.RegStat = 1 }
		if m.SMSStorageMax == 0    { m.SMSStorageMax = 30 }
		if m.ResponseDelayMs == 0  { m.ResponseDelayMs = 5 }
	}
}

func validate(cfg *EmuConfig) error {
	if len(cfg.Modems) == 0 {
		return fmt.Errorf("at least one modem must be configured")
	}
	kind := strings.ToLower(cfg.Transport.Kind)
	if kind != "unix" && kind != "tcp" {
		return fmt.Errorf("transport.kind must be \"unix\" or \"tcp\", got %q", kind)
	}
	cfg.Transport.Kind = kind
	for i, m := range cfg.Modems {
		if l := len(m.ICCID); l < 19 || l > 20 {
			return fmt.Errorf("modems[%d].iccid must be 19-20 digits, got len=%d", i, l)
		}
	}
	return nil
}

// Load reads and validates the config from a JSON file.
func Load(path string) (*EmuConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg EmuConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config JSON: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, validate(&cfg)
}

// AddrForModem returns the listen address for modem at index i.
func (cfg *EmuConfig) AddrForModem(i int) string {
	switch cfg.Transport.Kind {
	case "unix":
		return fmt.Sprintf("%s-%d.sock", cfg.Transport.UnixBasePath, i)
	case "tcp":
		return fmt.Sprintf("%s:%d", cfg.Transport.TCPBindAddr, cfg.Transport.TCPBasePort+i)
	}
	return ""
}

// GatewayConfigHint returns a YAML snippet the user pastes into go-sms-gate config.yaml.
func (cfg *EmuConfig) GatewayConfigHint() string {
	var sb strings.Builder
	sb.WriteString("modems:\n")
	for i, m := range cfg.Modems {
		addr := cfg.AddrForModem(i)
		sb.WriteString(fmt.Sprintf("  - transport: %s\n", cfg.Transport.Kind))
		sb.WriteString(fmt.Sprintf("    addr: %s\n", addr))
		sb.WriteString(fmt.Sprintf("    # ICCID: %s  profile: %s\n", m.ICCID, m.Profile))
	}
	return sb.String()
}
