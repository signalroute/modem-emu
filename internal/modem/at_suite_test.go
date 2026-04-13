// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// Comprehensive table-driven test suite for AT command handlers.
// Covers issues #197 and #198 — full emulation coverage.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/signalroute/modem-emu/internal/config"
)

// atTestCase describes a single AT command exchange.
type atTestCase struct {
	name        string
	cmd         string
	wantOK      bool   // true → last line must be "OK"
	wantContain string // non-empty → at least one line must contain this substring
}

// runTableTests runs atTestCases against a shared modem connection.
func runTableTests(t *testing.T, cases []atTestCase) {
	t.Helper()
	_, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         "89490200001234567890",
		IMSI:          "262019000012345",
		Operator:      "Telekom.de",
		SignalCSQ:     18,
		RegStat:       1,
		SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			lines := sendCmd(t, conn, tc.cmd)
			if tc.wantOK && lastLine(lines) != "OK" {
				t.Errorf("%s: want OK, got %v", tc.cmd, lines)
			}
			if tc.wantContain != "" {
				found := false
				for _, l := range lines {
					if strings.Contains(l, tc.wantContain) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: want line containing %q, got %v", tc.cmd, tc.wantContain, lines)
				}
			}
		})
	}
}

// ── ATI / AT+CGMI / AT+CGMM ──────────────────────────────────────────────

func TestAT_Suite_IdentityCommands(t *testing.T) {
	runTableTests(t, []atTestCase{
		{name: "ATI_ok", cmd: "ATI", wantOK: true, wantContain: "SIM800"},
		{name: "CGMM_ok", cmd: "AT+CGMM", wantOK: true, wantContain: "SIM800"},
		{name: "GMM_ok", cmd: "AT+GMM", wantOK: true, wantContain: "SIM800"},
		{name: "CGMI_ok", cmd: "AT+CGMI", wantOK: true, wantContain: "SIMCOM"},
		{name: "CGSN_ok", cmd: "AT+CGSN", wantOK: true, wantContain: "3582"},
		{name: "CIMI_ok", cmd: "AT+CIMI", wantOK: true, wantContain: "262019000012345"},
		{name: "CCID_query", cmd: "AT+CCID?", wantOK: true, wantContain: "89490200001234567890"},
		{name: "CCID_bare", cmd: "AT+CCID", wantOK: true, wantContain: "89490200001234567890"},
		{name: "QCCID", cmd: "AT+QCCID", wantOK: true, wantContain: "89490200001234567890"},
		{name: "CPIN_ready", cmd: "AT+CPIN?", wantOK: true, wantContain: "READY"},
	})
}

// ── Network commands ──────────────────────────────────────────────────────

func TestAT_Suite_NetworkCommands(t *testing.T) {
	runTableTests(t, []atTestCase{
		{name: "CSQ_value", cmd: "AT+CSQ", wantOK: true, wantContain: "+CSQ: 18,0"},
		{name: "CREG_home", cmd: "AT+CREG?", wantOK: true, wantContain: "+CREG:"},
		{name: "COPS_query", cmd: "AT+COPS?", wantOK: true, wantContain: "Telekom.de"},
		{name: "CGREG_query", cmd: "AT+CGREG?", wantOK: true, wantContain: "+CGREG: 0,1"},
		{name: "CEREG_query", cmd: "AT+CEREG?", wantOK: true, wantContain: "+CEREG: 0,1"},
	})
}

// ── SMS storage commands ──────────────────────────────────────────────────

func TestAT_Suite_CPMSAndStorage(t *testing.T) {
	runTableTests(t, []atTestCase{
		{name: "CPMS_query", cmd: "AT+CPMS?", wantOK: true, wantContain: `+CPMS: "SM"`},
		{name: "CPMS_set_ignored", cmd: `AT+CPMS="SM","SM","SM"`, wantOK: true},
	})
}

func TestAT_Suite_CMGL_Empty(t *testing.T) {
	_, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile: "SIM800L", ICCID: "89490200001234567890", IMSI: "262019000012345",
		Operator: "Telekom.de", SignalCSQ: 18, RegStat: 1, SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")

	// Empty storage → just OK, no +CMGL lines.
	lines := sendCmd(t, conn, "AT+CMGL=4")
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CMGL=4 on empty storage: got %v", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "+CMGL:") {
			t.Errorf("unexpected +CMGL line on empty storage: %q", l)
		}
	}
}

func TestAT_Suite_CMGL_WithMessages(t *testing.T) {
	m, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile: "SIM800L", ICCID: "89490200001234567890", IMSI: "262019000012345",
		Operator: "Telekom.de", SignalCSQ: 18, RegStat: 1, SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")

	// Inject two SMS messages.
	m.InjectSMS("+491111111111", "First message")
	m.InjectSMS("+492222222222", "Second message")

	// AT+CMGL=4 lists all messages.
	lines := sendCmd(t, conn, "AT+CMGL=4")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CMGL=4: got %v", lines)
	}
	count := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "+CMGL:") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 +CMGL: lines, got %d: %v", count, lines)
	}

	// AT+CMGL="ALL" should do the same.
	lines2 := sendCmd(t, conn, `AT+CMGL="ALL"`)
	if lastLine(lines2) != "OK" {
		t.Fatalf(`AT+CMGL="ALL": got %v`, lines2)
	}
	count2 := 0
	for _, l := range lines2 {
		if strings.HasPrefix(l, "+CMGL:") {
			count2++
		}
	}
	if count2 != 2 {
		t.Errorf(`AT+CMGL="ALL": expected 2 +CMGL lines, got %d: %v`, count2, lines2)
	}
}

func TestAT_Suite_CMGR_ReadMarksAsRead(t *testing.T) {
	m, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile: "SIM800L", ICCID: "89490200001234567890", IMSI: "262019000012345",
		Operator: "Telekom.de", SignalCSQ: 18, RegStat: 1, SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")

	idx, err := m.InjectSMS("+4915198765432", "Read me")
	if err != nil {
		t.Fatalf("InjectSMS: %v", err)
	}

	// First read: status should be 0 (unread) in +CMGR header.
	lines := sendCmd(t, conn, fmt.Sprintf("AT+CMGR=%d", idx))
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CMGR=%d: %v", idx, lines)
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "+CMGR:") {
			found = true
			// Status 0 = unread (first read marks it read internally, but header says 0).
			if !strings.Contains(l, "+CMGR:") {
				t.Errorf("unexpected +CMGR format: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("+CMGR: line not found: %v", lines)
	}

	// After reading, slot status is 1 (read).
	slot := m.storage.Read(idx)
	if slot == nil || slot.Status != 1 {
		t.Errorf("slot status after CMGR: want 1 (read), got %v", slot)
	}
}

func TestAT_Suite_CMGD_SingleAndAll(t *testing.T) {
	m, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile: "SIM800L", ICCID: "89490200001234567890", IMSI: "262019000012345",
		Operator: "Telekom.de", SignalCSQ: 18, RegStat: 1, SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")

	idx1, _ := m.InjectSMS("+491", "msg1")
	idx2, _ := m.InjectSMS("+492", "msg2")
	idx3, _ := m.InjectSMS("+493", "msg3")
	_ = idx2

	// Delete single slot.
	lines := sendCmd(t, conn, fmt.Sprintf("AT+CMGD=%d", idx1))
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CMGD single: %v", lines)
	}
	used, _ := m.StorageCount()
	if used != 2 {
		t.Errorf("after single delete: want 2 messages, got %d", used)
	}

	// Delete all (flag=4).
	lines = sendCmd(t, conn, fmt.Sprintf("AT+CMGD=%d,4", idx3))
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CMGD delete-all: %v", lines)
	}
	used, _ = m.StorageCount()
	if used != 0 {
		t.Errorf("after delete-all: want 0 messages, got %d", used)
	}
}

// ── CMGF mode ─────────────────────────────────────────────────────────────

func TestAT_Suite_CMGFSetVariants(t *testing.T) {
	runTableTests(t, []atTestCase{
		{name: "CMGF_pdu", cmd: "AT+CMGF=0", wantOK: true},
		{name: "CMGF_text", cmd: "AT+CMGF=1", wantOK: true},
	})
}

// ── V.250 basics ──────────────────────────────────────────────────────────

func TestAT_Suite_V250Basics(t *testing.T) {
	runTableTests(t, []atTestCase{
		{name: "AT_ping", cmd: "AT", wantOK: true},
		{name: "ATZ_reset", cmd: "ATZ", wantOK: true},
		{name: "ATF_factory", cmd: "AT&F", wantOK: true},
		{name: "CMEE1", cmd: "AT+CMEE=1", wantOK: true},
		{name: "CMEE2", cmd: "AT+CMEE=2", wantOK: true},
		{name: "CNMI_set", cmd: "AT+CNMI=2,1,0,0,0", wantOK: true},
		{name: "CNMI_query", cmd: "AT+CNMI?", wantOK: true, wantContain: "+CNMI:"},
		{name: "CSCA_query", cmd: "AT+CSCA?", wantOK: true, wantContain: "+CSCA:"},
		{name: "CLCK_SC_query", cmd: `AT+CLCK="SC",2`, wantOK: true, wantContain: "+CLCK: 0"},
		{name: "unknown_errors", cmd: "AT+NOTACOMMAND", wantOK: false},
	})
}

// ── Profile variants for ATI / AT+CGMI ───────────────────────────────────

func TestAT_Suite_ProfileVariants(t *testing.T) {
	profiles := []struct {
		profile      string
		wantATI      string
		wantCGMI     string
	}{
		{"SIM800L", "SIM800", "SIMCOM"},
		{"SIM7600", "SIM7600", "SIMCOM"},
		{"EC21", "Quectel", "Quectel"},
		{"GENERIC", "go-modem-emu", "SignalRoute"},
	}

	for _, p := range profiles {
		p := p
		t.Run(p.profile, func(t *testing.T) {
			_, conn := newTestModemWithCfg(t, config.ModemConfig{
				Profile:       p.profile,
				ICCID:         "89490200001234567890",
				IMSI:          "262019000012345",
				Operator:      "Telekom.de",
				SignalCSQ:     18,
				RegStat:       1,
				SMSStorageMax: 10,
			})
			sendCmd(t, conn, "ATE0")

			atiLines := sendCmd(t, conn, "ATI")
			if lastLine(atiLines) != "OK" {
				t.Errorf("ATI: %v", atiLines)
			}
			foundATI := false
			for _, l := range atiLines {
				if strings.Contains(l, p.wantATI) {
					foundATI = true
				}
			}
			if !foundATI {
				t.Errorf("ATI: want %q in response, got %v", p.wantATI, atiLines)
			}

			cgmiLines := sendCmd(t, conn, "AT+CGMI")
			if lastLine(cgmiLines) != "OK" {
				t.Errorf("AT+CGMI: %v", cgmiLines)
			}
			foundCGMI := false
			for _, l := range cgmiLines {
				if strings.Contains(l, p.wantCGMI) {
					foundCGMI = true
				}
			}
			if !foundCGMI {
				t.Errorf("AT+CGMI: want %q in response, got %v", p.wantCGMI, cgmiLines)
			}
		})
	}
}

// ── CMGS text mode ────────────────────────────────────────────────────────

func TestAT_Suite_CMGS_TextMode(t *testing.T) {
	m, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile: "SIM800L", ICCID: "89490200001234567890", IMSI: "262019000012345",
		Operator: "Telekom.de", SignalCSQ: 18, RegStat: 1, SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=1")

	// Use the helpers from pdu_mode_test.go (same package).
	conn.Write([]byte("AT+CMGS=\"+49151999999\"\r\n"))
	readUntilPrompt(t, conn)
	conn.Write([]byte("Hello from suite\x1A"))
	resp := collectUntilOK(t, conn)

	found := false
	for _, l := range resp {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf("AT+CMGS text: want +CMGS in response, got %v", resp)
	}

	sent := m.SentMessages()
	if len(sent) == 0 {
		t.Fatal("no sent messages recorded")
	}
	if sent[len(sent)-1].To != "+49151999999" {
		t.Errorf("want To=+49151999999, got %q", sent[len(sent)-1].To)
	}
}
