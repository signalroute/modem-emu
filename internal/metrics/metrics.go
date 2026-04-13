// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package metrics exposes emulator runtime metrics via expvar over HTTP.
// Metrics are available at GET /metrics on the address controlled by the
// METRICS_ADDR environment variable (default ":9201").
package metrics

import (
	"expvar"
	"net/http"
	"os"
)

var (
	// ActiveConnections is the number of currently connected AT clients.
	ActiveConnections = expvar.NewInt("modememu_active_connections")

	// ATCommandsTotal counts AT commands by base command name (e.g. "AT+CSQ").
	ATCommandsTotal = expvar.NewMap("modememu_at_commands_total")

	// SMSInjectedTotal counts inbound SMS messages injected via the control API.
	SMSInjectedTotal = expvar.NewInt("modememu_sms_injected_total")
)

// Addr returns the listen address for the metrics server, honouring
// the METRICS_ADDR environment variable (default ":9201").
func Addr() string {
	if v := os.Getenv("METRICS_ADDR"); v != "" {
		return v
	}
	return ":9201"
}

// Handler returns an http.Handler that serves the expvar metrics page at /metrics.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", expvar.Handler())
	return mux
}
