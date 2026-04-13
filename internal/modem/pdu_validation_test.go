// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// Tests for PDU mode validation fixes.
// Covers issues #196 (length mismatch → ERROR) and #200 (Ctrl-Z / \x1a text escape handling).

import (
	"strings"
	"testing"
	"time"

	"github.com/signalroute/modem-emu/internal/config"
)

const (
	// 20-byte PDU used across tests.
	validPDUHex = "0001000B914991517654F20000AA05C8329BFD06"
	validPDULen = 20
)

// helper: send AT+CMGS in PDU mode and return the final response lines.
func sendPDU(t *testing.T, conn interface {
	Write(b []byte) (int, error)
	Read(b []byte) (int, error)
	SetDeadline(t time.Time) error
}, declaredLen int, pduBody string, terminator string) []string {
	t.Helper()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	conn.Write([]byte("AT+CMGS=" + itoa(declaredLen) + "\r\n"))
	readUntilPrompt(t, conn)
	conn.Write([]byte(pduBody + terminator))
	return collectUntilOK(t, conn)
}

// ── Issue #196: PDU length mismatch ──────────────────────────────────────

// TestPDUValidation_CorrectLength verifies that a PDU with the correct declared
// length succeeds.
func TestPDUValidation_CorrectLength(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	lines := sendPDU(t, conn, validPDULen, validPDUHex, "\x1A")

	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf("correct length: expected +CMGS: in response, got %v", lines)
	}
}

// TestPDUValidation_LengthTooShort rejects when declared length < actual PDU bytes.
func TestPDUValidation_LengthTooShort(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	// Declare 10 bytes but send 20.
	lines := sendPDU(t, conn, 10, validPDUHex, "\x1A")

	if !containsError(lines) {
		t.Errorf("length too short: expected ERROR, got %v", lines)
	}
}

// TestPDUValidation_LengthTooLong rejects when declared length > actual PDU bytes.
func TestPDUValidation_LengthTooLong(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	// Declare 30 bytes but send 20.
	lines := sendPDU(t, conn, 30, validPDUHex, "\x1A")

	if !containsError(lines) {
		t.Errorf("length too long: expected ERROR, got %v", lines)
	}
}

// TestPDUValidation_OddHexLength rejects a PDU hex with odd number of chars (invalid hex).
func TestPDUValidation_OddHexLength(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	// 41 hex chars = odd → not valid hex byte stream.
	oddPDU := validPDUHex + "A" // one extra nibble
	lines := sendPDU(t, conn, validPDULen, oddPDU, "\x1A")

	if !containsError(lines) {
		t.Errorf("odd hex: expected ERROR, got %v", lines)
	}
}

// TestPDUValidation_MultipleSuccessiveSends checks the modem remains healthy
// after a rejected send (next valid send succeeds).
func TestPDUValidation_MultipleSuccessiveSends(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	// First send: wrong length → ERROR.
	lines := sendPDU(t, conn, 5, validPDUHex, "\x1A")
	if !containsError(lines) {
		t.Fatalf("first send (wrong len): expected ERROR, got %v", lines)
	}

	// Second send: correct length → OK.
	lines = sendPDU(t, conn, validPDULen, validPDUHex, "\x1A")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf("second send (correct): expected +CMGS:, got %v", lines)
	}
}

// ── Issue #200: Ctrl-Z / \x1a text escape handling ───────────────────────

// TestPDUValidation_CtrlZByte verifies that the binary Ctrl-Z byte (0x1A)
// correctly terminates the PDU input (no hang).
func TestPDUValidation_CtrlZByte(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	lines := sendPDU(t, conn, validPDULen, validPDUHex, "\x1A")

	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf("Ctrl-Z byte: expected +CMGS:, got %v", lines)
	}
}

// TestPDUValidation_TextEscapeX1a verifies that the literal 4-char "\x1a"
// text escape is stripped correctly and the PDU is accepted.
func TestPDUValidation_TextEscapeX1a(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	// Send PDU with literal "\x1a" (4 chars) as terminator, followed by \r\n.
	lines := sendPDU(t, conn, validPDULen, validPDUHex+`\x1a`, "\r\n")

	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf(`literal \x1a escape: expected +CMGS:, got %v`, lines)
	}
}

// TestPDUValidation_TextEscapeX1aUpperCase verifies upper-case "\X1A" is also stripped.
func TestPDUValidation_TextEscapeX1aUpperCase(t *testing.T) {
	_, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	lines := sendPDU(t, conn, validPDULen, validPDUHex+`\X1A`, "\r\n")

	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf(`upper-case \X1A escape: expected +CMGS:, got %v`, lines)
	}
}

// TestPDUValidation_TextMode_CtrlZ confirms text mode is unaffected by the fix.
func TestPDUValidation_TextMode_CtrlZ(t *testing.T) {
	m, conn := newTestModemWithCfg(t, defaultPDUCfg(t))
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=1")

	conn.SetDeadline(time.Now().Add(3 * time.Second))
	conn.Write([]byte("AT+CMGS=\"+49111222333\"\r\n"))
	readUntilPrompt(t, conn)
	conn.Write([]byte("PDU validation test message\x1A"))
	resp := collectUntilOK(t, conn)

	found := false
	for _, l := range resp {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf("text mode Ctrl-Z: expected +CMGS:, got %v", resp)
	}

	sent := m.SentMessages()
	if len(sent) == 0 {
		t.Fatal("no sent messages in text mode")
	}
	if sent[len(sent)-1].Body != "PDU validation test message" {
		t.Errorf("text body: got %q", sent[len(sent)-1].Body)
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

func defaultPDUCfg(t *testing.T) config.ModemConfig {
	t.Helper()
	return config.ModemConfig{
		Profile: "SIM800L", ICCID: "89490200001234567890", IMSI: "262019000012345",
		Operator: "Telekom.de", SignalCSQ: 18, RegStat: 1, SMSStorageMax: 10,
	}
}

func containsError(lines []string) bool {
	for _, l := range lines {
		if l == "ERROR" || strings.HasPrefix(l, "+CMS ERROR") || strings.HasPrefix(l, "+CME ERROR") {
			return true
		}
	}
	return false
}
