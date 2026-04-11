// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"

	config "github.com/signalroute/modem-emu/internal/config"
)

// newTestModemWithCfg creates a Modem with a custom ModemConfig, connected via net.Pipe.
func newTestModemWithCfg(t *testing.T, cfg config.ModemConfig) (*Modem, net.Conn) {
	t.Helper()
	if cfg.SMSStorageMax == 0 {
		cfg.SMSStorageMax = 10
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := New(cfg, log)

	clientConn, serverConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		clientConn.Close()
		serverConn.Close()
	})
	go m.RunSession(ctx, serverConn)
	return m, clientConn
}

// TestATCommand_ManyICCIDs creates 200 modems with distinct ICCIDs and queries AT+CCID?.
func TestATCommand_ManyICCIDs(t *testing.T) {
	for i := 0; i < 200; i++ {
		i := i
		t.Run(fmt.Sprintf("iccid_%d", i), func(t *testing.T) {
			iccid := fmt.Sprintf("8949020000123%07d", i)
			_, conn := newTestModemWithCfg(t, config.ModemConfig{
				Profile:   "SIM800L",
				ICCID:     iccid,
				IMSI:      "262019000012345",
				Operator:  "Telekom.de",
				SignalCSQ: 18,
				RegStat:   1,
			})
			// Disable echo
			sendCmd(t, conn, "ATE0")

			lines := sendCmd(t, conn, "AT+CCID?")
			if lastLine(lines) != "OK" {
				t.Fatalf("AT+CCID? did not return OK: %v", lines)
			}
			found := false
			for _, l := range lines {
				if strings.Contains(l, iccid) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ICCID %q not found in response: %v", iccid, lines)
			}
		})
	}
}

// TestATCommand_UnknownCommands sends 100 unknown commands to one shared modem
// and verifies each returns ERROR.
func TestATCommand_UnknownCommands(t *testing.T) {
	_, conn := testModem(t)
	// Disable echo so response lines are clean.
	sendCmd(t, conn, "ATE0")

	for i := 0; i < 100; i++ {
		i := i
		t.Run(fmt.Sprintf("unknown_%d", i), func(t *testing.T) {
			cmd := fmt.Sprintf("AT+UNKNOWN%03d", i)
			lines := sendCmd(t, conn, cmd)
			if lastLine(lines) != "ERROR" {
				t.Errorf("%q: expected ERROR, got %v", cmd, lines)
			}
		})
	}
}

// TestATCommand_CSQVariants creates one modem per CSQ value 0-31 and verifies AT+CSQ response.
func TestATCommand_CSQVariants(t *testing.T) {
	for csq := 0; csq <= 31; csq++ {
		csq := csq
		t.Run(fmt.Sprintf("csq_%d", csq), func(t *testing.T) {
			_, conn := newTestModemWithCfg(t, config.ModemConfig{
				Profile:   "SIM800L",
				ICCID:     fmt.Sprintf("89490200001234%06d", csq),
				IMSI:      "262019000012345",
				Operator:  "Telekom.de",
				SignalCSQ: csq,
				RegStat:   1,
			})
			sendCmd(t, conn, "ATE0")

			lines := sendCmd(t, conn, "AT+CSQ")
			if lastLine(lines) != "OK" {
				t.Fatalf("AT+CSQ did not return OK: %v", lines)
			}
			expected := fmt.Sprintf("+CSQ: %d,0", csq)
			found := false
			for _, l := range lines {
				if strings.Contains(l, expected) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("CSQ %d: expected %q in response %v", csq, expected, lines)
			}
		})
	}
}

// TestATCommand_RegStatVariants creates one modem per reg stat 0-7 and verifies AT+CREG?.
func TestATCommand_RegStatVariants(t *testing.T) {
	for stat := 0; stat <= 7; stat++ {
		stat := stat
		t.Run(fmt.Sprintf("regstat_%d", stat), func(t *testing.T) {
			_, conn := newTestModemWithCfg(t, config.ModemConfig{
				Profile:   "SIM800L",
				ICCID:     fmt.Sprintf("89490200001235%06d", stat),
				IMSI:      "262019000012345",
				Operator:  "Telekom.de",
				SignalCSQ: 18,
				RegStat:   stat,
			})
			sendCmd(t, conn, "ATE0")

			lines := sendCmd(t, conn, "AT+CREG?")
			if lastLine(lines) != "OK" {
				t.Fatalf("AT+CREG? stat=%d did not return OK: %v", stat, lines)
			}
			expected := fmt.Sprintf("%d", stat)
			found := false
			for _, l := range lines {
				if strings.Contains(l, expected) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("regstat %d: expected %q in response %v", stat, expected, lines)
			}
		})
	}
}

// TestATCommand_OperatorVariants creates one modem per operator (20 variants) and verifies AT+COPS?.
func TestATCommand_OperatorVariants(t *testing.T) {
	operators := []string{
		"Telekom.de", "Vodafone.de", "O2.de", "E-Plus",
		"Orange.fr", "SFR.fr", "Bouygues", "T-Mobile.pl",
		"Plus.pl", "Play.pl", "Wind.it", "TIM.it",
		"Vodafone.it", "Movistar.es", "Orange.es", "Yoigo.es",
		"Swisscom.ch", "Sunrise.ch", "Salt.ch", "A1.at",
	}

	for i, op := range operators {
		i, op := i, op
		t.Run(fmt.Sprintf("operator_%d", i), func(t *testing.T) {
			_, conn := newTestModemWithCfg(t, config.ModemConfig{
				Profile:   "SIM800L",
				ICCID:     fmt.Sprintf("89490200001236%06d", i),
				IMSI:      "262019000012345",
				Operator:  op,
				SignalCSQ: 18,
				RegStat:   1,
			})
			sendCmd(t, conn, "ATE0")

			lines := sendCmd(t, conn, "AT+COPS?")
			if lastLine(lines) != "OK" {
				t.Fatalf("AT+COPS? op=%q did not return OK: %v", op, lines)
			}
			found := false
			for _, l := range lines {
				if strings.Contains(l, op) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("operator %q not found in response %v", op, lines)
			}
		})
	}
}

// TestATCommand_PingManyTimes sends AT to a single modem 200 times and verifies OK each time.
func TestATCommand_PingManyTimes(t *testing.T) {
_, conn := testModem(t)
sendCmd(t, conn, "ATE0")

for i := 0; i < 200; i++ {
i := i
t.Run(fmt.Sprintf("ping_%d", i), func(t *testing.T) {
lines := sendCmd(t, conn, "AT")
if lastLine(lines) != "OK" {
t.Errorf("ping %d: expected OK, got %v", i, lines)
}
})
}
}
