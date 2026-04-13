// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// Tests for AT+CGREG set variants, AT+CEREG set variants, and AT+CGATT.
// Covers issues #189 and #190.

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/signalroute/modem-emu/internal/config"
)

func newRegModem(t *testing.T, regStat int) (*Modem, net.Conn) {
	t.Helper()
	return newTestModemWithCfg(t, config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         fmt.Sprintf("894902000012345%05d", regStat),
		IMSI:          "262019000012345",
		Operator:      "Telekom.de",
		SignalCSQ:     18,
		RegStat:       regStat,
		SMSStorageMax: 10,
	})
}

// ── AT+CGREG set variants ─────────────────────────────────────────────────

func TestAT_CGREG_Set0(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CGREG=0")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CGREG=0: got %v", lines)
	}
}

func TestAT_CGREG_Set1(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CGREG=1")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CGREG=1: got %v", lines)
	}
}

func TestAT_CGREG_Set2(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CGREG=2")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CGREG=2: got %v", lines)
	}
}

// TestAT_CGREG_SetDoesNotChangeRegStat verifies that AT+CGREG=<n> (mode set)
// does not alter the underlying registration status.
func TestAT_CGREG_SetDoesNotChangeRegStat(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CGREG=2")

	lines := sendCmd(t, conn, "AT+CGREG?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CGREG? after set: %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CGREG:") && strings.Contains(l, "1") {
			found = true
		}
	}
	if !found {
		t.Errorf("regstat should still be 1 after AT+CGREG=2: %v", lines)
	}
}

// ── AT+CEREG set variants ─────────────────────────────────────────────────

func TestAT_CEREG_Set0(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CEREG=0")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CEREG=0: got %v", lines)
	}
}

func TestAT_CEREG_Set1(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CEREG=1")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CEREG=1: got %v", lines)
	}
}

func TestAT_CEREG_Set2(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CEREG=2")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CEREG=2: got %v", lines)
	}
}

func TestAT_CEREG_Set3(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CEREG=3")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CEREG=3: got %v", lines)
	}
}

// TestAT_CEREG_SetDoesNotChangeRegStat mirrors the CGREG check for CEREG.
func TestAT_CEREG_SetDoesNotChangeRegStat(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CEREG=3")

	lines := sendCmd(t, conn, "AT+CEREG?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CEREG? after set: %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CEREG:") && strings.Contains(l, "1") {
			found = true
		}
	}
	if !found {
		t.Errorf("regstat should still be 1 after AT+CEREG=3: %v", lines)
	}
}

// ── AT+CGATT ─────────────────────────────────────────────────────────────

func TestAT_CGATT_Query(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CGATT?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CGATT?: got %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CGATT: 1") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CGATT: 1 not found in: %v", lines)
	}
}

func TestAT_CGATT_Set0(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CGATT=0")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CGATT=0: got %v", lines)
	}
}

func TestAT_CGATT_Set1(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CGATT=1")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CGATT=1: got %v", lines)
	}
}

// TestAT_CGATT_SetThenQuery verifies the query response after a set command.
func TestAT_CGATT_SetThenQuery(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CGATT=1") // set (no-op in emulator)

	lines := sendCmd(t, conn, "AT+CGATT?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CGATT? after set: %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CGATT: 1") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CGATT: 1 expected after set=1: %v", lines)
	}
}

// TestAT_CGATT_InvalidSet verifies unknown CGATT values are rejected.
func TestAT_CGATT_InvalidSet(t *testing.T) {
	_, conn := newRegModem(t, 1)
	sendCmd(t, conn, "ATE0")
	lines := sendCmd(t, conn, "AT+CGATT=2")
	if lastLine(lines) != "ERROR" {
		t.Errorf("AT+CGATT=2 (invalid): expected ERROR, got %v", lines)
	}
}

// ── Combined sequence test ────────────────────────────────────────────────

// TestAT_RegistrationCommandSequence exercises the full sequence a real
// GPRS stack would issue at startup.
func TestAT_RegistrationCommandSequence(t *testing.T) {
	_, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         "89490200001234567890",
		IMSI:          "262019000012345",
		Operator:      "Telekom.de",
		SignalCSQ:     20,
		RegStat:       1,
		SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")

	cmds := []struct {
		cmd     string
		wantOK  bool
		contain string
	}{
		{"AT+CREG?", true, "+CREG:"},
		{"AT+CGREG?", true, "+CGREG: 0,1"},
		{"AT+CGREG=0", true, ""},
		{"AT+CGREG=1", true, ""},
		{"AT+CGREG=2", true, ""},
		{"AT+CEREG?", true, "+CEREG: 0,1"},
		{"AT+CEREG=0", true, ""},
		{"AT+CEREG=1", true, ""},
		{"AT+CEREG=2", true, ""},
		{"AT+CEREG=3", true, ""},
		{"AT+CGATT?", true, "+CGATT: 1"},
		{"AT+CGATT=1", true, ""},
		{"AT+CGATT=0", true, ""},
	}

	for _, c := range cmds {
		c := c
		t.Run(c.cmd, func(t *testing.T) {
			lines := sendCmd(t, conn, c.cmd)
			if c.wantOK && lastLine(lines) != "OK" {
				t.Errorf("%s: want OK, got %v", c.cmd, lines)
			}
			if c.contain != "" {
				found := false
				for _, l := range lines {
					if strings.Contains(l, c.contain) {
						found = true
					}
				}
				if !found {
					t.Errorf("%s: want line with %q in %v", c.cmd, c.contain, lines)
				}
			}
		})
	}
}
