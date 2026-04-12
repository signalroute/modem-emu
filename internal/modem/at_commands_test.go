// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// Tests for newly added AT commands: AT+CCID (bare), AT+CGREG?, AT+CEREG?.
// Addresses issues #182, #190, #189.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/signalroute/modem-emu/internal/config"
)

// TestAT_CCID_Bare verifies AT+CCID (without trailing ?) returns the ICCID.
func TestAT_CCID_Bare(t *testing.T) {
	_, conn := testModem(t)
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CCID")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CCID: got %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "89490200001234567890") {
			found = true
		}
	}
	if !found {
		t.Errorf("ICCID not found in AT+CCID response: %v", lines)
	}
}

// TestAT_CGREG_HomeNetwork verifies AT+CGREG? returns +CGREG: 0,1 for reg_stat=1.
func TestAT_CGREG_HomeNetwork(t *testing.T) {
	_, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         "89490200001234567801",
		IMSI:          "262019000012345",
		Operator:      "Telekom.de",
		SignalCSQ:     18,
		RegStat:       1,
	})
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CGREG?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CGREG?: got %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CGREG:") && strings.Contains(l, "1") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CGREG: 0,1 not found in response: %v", lines)
	}
}

// TestAT_CEREG_HomeNetwork verifies AT+CEREG? returns +CEREG: 0,1 for reg_stat=1.
func TestAT_CEREG_HomeNetwork(t *testing.T) {
	_, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         "89490200001234567802",
		IMSI:          "262019000012345",
		Operator:      "Telekom.de",
		SignalCSQ:     18,
		RegStat:       1,
	})
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CEREG?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CEREG?: got %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CEREG:") && strings.Contains(l, "1") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CEREG: 0,1 not found in response: %v", lines)
	}
}

// TestAT_CGREG_ReflectsRegStat verifies that AT+CGREG? reports the current
// registration stat (not a hardcoded value).
func TestAT_CGREG_ReflectsRegStat(t *testing.T) {
	for _, stat := range []int{0, 1, 2, 5} {
		stat := stat
		t.Run(fmt.Sprintf("stat_%d", stat), func(t *testing.T) {
			m, conn := newTestModemWithCfg(t, config.ModemConfig{
				Profile:       "SIM800L",
				ICCID:         fmt.Sprintf("89490200001234%06d", stat),
				IMSI:          "262019000012345",
				Operator:      "Telekom.de",
				SignalCSQ:     18,
				RegStat:       stat,
			})
			_ = m
			sendCmd(t, conn, "ATE0")

			lines := sendCmd(t, conn, "AT+CGREG?")
			if lastLine(lines) != "OK" {
				t.Fatalf("stat=%d AT+CGREG?: got %v", stat, lines)
			}
			expected := fmt.Sprintf("%d", stat)
			found := false
			for _, l := range lines {
				if strings.Contains(l, "+CGREG:") && strings.Contains(l, expected) {
					found = true
				}
			}
			if !found {
				t.Errorf("stat=%d: expected %q in +CGREG response: %v", stat, expected, lines)
			}
		})
	}
}

// TestAT_CEREG_ReflectsRegStat verifies that AT+CEREG? reports the current
// registration stat.
func TestAT_CEREG_ReflectsRegStat(t *testing.T) {
	for _, stat := range []int{0, 1, 2, 5} {
		stat := stat
		t.Run(fmt.Sprintf("stat_%d", stat), func(t *testing.T) {
			m, conn := newTestModemWithCfg(t, config.ModemConfig{
				Profile:       "SIM800L",
				ICCID:         fmt.Sprintf("89490200001235%06d", stat),
				IMSI:          "262019000012345",
				Operator:      "Telekom.de",
				SignalCSQ:     18,
				RegStat:       stat,
			})
			_ = m
			sendCmd(t, conn, "ATE0")

			lines := sendCmd(t, conn, "AT+CEREG?")
			if lastLine(lines) != "OK" {
				t.Fatalf("stat=%d AT+CEREG?: got %v", stat, lines)
			}
			expected := fmt.Sprintf("%d", stat)
			found := false
			for _, l := range lines {
				if strings.Contains(l, "+CEREG:") && strings.Contains(l, expected) {
					found = true
				}
			}
			if !found {
				t.Errorf("stat=%d: expected %q in +CEREG response: %v", stat, expected, lines)
			}
		})
	}
}
