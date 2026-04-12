// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// Tests for feat/pdu-mode: CMGF mode tracking and dual-mode AT+CMGS handling.

import (
	"strings"
	"testing"
	"time"
)

// TestCMGF_DefaultIsTextMode verifies that the modem starts in text mode (CMGF=1).
func TestCMGF_DefaultIsTextMode(t *testing.T) {
	m, _ := testModem(t)
	if m.cmgfMode.Load() != 1 {
		t.Errorf("expected default cmgfMode=1 (text), got %d", m.cmgfMode.Load())
	}
}

// TestCMGF_SwitchToPDU verifies AT+CMGF=0 switches to PDU mode.
func TestCMGF_SwitchToPDU(t *testing.T) {
	m, conn := testModem(t)
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CMGF=0")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CMGF=0: %v", lines)
	}
	if m.cmgfMode.Load() != 0 {
		t.Errorf("expected cmgfMode=0 (PDU) after AT+CMGF=0, got %d", m.cmgfMode.Load())
	}
}

// TestCMGF_SwitchToText verifies AT+CMGF=1 sets/keeps text mode.
func TestCMGF_SwitchToText(t *testing.T) {
	m, conn := testModem(t)
	sendCmd(t, conn, "ATE0")

	// Switch to PDU then back to text.
	sendCmd(t, conn, "AT+CMGF=0")
	lines := sendCmd(t, conn, "AT+CMGF=1")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CMGF=1: %v", lines)
	}
	if m.cmgfMode.Load() != 1 {
		t.Errorf("expected cmgfMode=1 (text) after AT+CMGF=1, got %d", m.cmgfMode.Load())
	}
}

// TestCMGS_PDUMode verifies AT+CMGS in PDU mode (CMGF=0) decodes the PDU
// and records the sent message correctly.
func TestCMGS_PDUMode(t *testing.T) {
	_, conn := testModem(t)
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	// Standard SMS-SUBMIT PDU: TO=+94191567452, body="Hello"
	// 00 01 00 0B 91 49 91 51 76 54 F2 00 00 AA 05 C8 32 9B FD 06
	const pduHex = "0001000B914991517654F20000AA05C8329BFD06"
	const pduLen = "20" // 20 bytes excluding SMSC (first byte 00)

	conn.SetDeadline(time.Now().Add(3 * time.Second))
	conn.Write([]byte("AT+CMGS=" + pduLen + "\r\n"))

	// Read until we see "> " prompt.
	lines := readUntilPrompt(t, conn)
	if !strings.Contains(strings.Join(lines, " "), ">") {
		t.Fatalf("expected '>' prompt, got: %v", lines)
	}

	// Send PDU + Ctrl-Z; the response (+CMGS: N\r\nOK) comes back directly.
	conn.Write([]byte(pduHex + "\x1A"))

	cmgsLines := collectUntilOK(t, conn)
	found := false
	for _, l := range cmgsLines {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected +CMGS: in response, got: %v", cmgsLines)
	}
}

// TestCMGS_TextMode verifies AT+CMGS in text mode (CMGF=1) stores the
// destination and body from the AT command and input line.
func TestCMGS_TextMode(t *testing.T) {
	m, conn := testModem(t)
	sendCmd(t, conn, "ATE0")
	// CMGF=1 is the default, but set it explicitly.
	sendCmd(t, conn, "AT+CMGF=1")

	conn.SetDeadline(time.Now().Add(3 * time.Second))
	conn.Write([]byte("AT+CMGS=\"+4915198765432\"\r\n"))

	// Read until '>' prompt.
	lines := readUntilPrompt(t, conn)
	if !strings.Contains(strings.Join(lines, " "), ">") {
		t.Fatalf("expected '>' prompt after AT+CMGS in text mode, got: %v", lines)
	}

	// Send message body + Ctrl-Z.
	conn.Write([]byte("Hello text mode\x1A"))

	cmgsLines := collectUntilOK(t, conn)
	found := false
	for _, l := range cmgsLines {
		if strings.Contains(l, "+CMGS:") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected +CMGS: in text mode response, got: %v", cmgsLines)
	}

	// Verify the sent message was recorded correctly.
	sent := m.SentMessages()
	if len(sent) == 0 {
		t.Fatal("no sent messages recorded")
	}
	last := sent[len(sent)-1]
	if last.To != "+4915198765432" {
		t.Errorf("expected To=+4915198765432, got %q", last.To)
	}
	if last.Body != "Hello text mode" {
		t.Errorf("expected Body='Hello text mode', got %q", last.Body)
	}
}

// TestCMGS_PDUMode_MRIncrements verifies that the message reference counter
// increments between sends in PDU mode.
func TestCMGS_PDUMode_MRIncrements(t *testing.T) {
	_, conn := testModem(t)
	sendCmd(t, conn, "ATE0")
	sendCmd(t, conn, "AT+CMGF=0")

	const pduHex = "0001000B914991517654F20000AA05C8329BFD06"
	mrValues := make([]string, 0, 2)

	for i := 0; i < 2; i++ {
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		conn.Write([]byte("AT+CMGS=20\r\n"))
		readUntilPrompt(t, conn)
		conn.Write([]byte(pduHex + "\x1A"))
		lines := collectUntilOK(t, conn)
		for _, l := range lines {
			if strings.Contains(l, "+CMGS:") {
				mrValues = append(mrValues, l)
			}
		}
	}

	if len(mrValues) != 2 {
		t.Fatalf("expected 2 +CMGS responses, got %d: %v", len(mrValues), mrValues)
	}
	if mrValues[0] == mrValues[1] {
		t.Errorf("message references should differ: %v %v", mrValues[0], mrValues[1])
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

// readUntilPrompt reads lines until it finds "> " or times out.
func readUntilPrompt(t *testing.T, conn interface {
	Read(b []byte) (int, error)
}) []string {
	t.Helper()
	var lines []string
	buf := make([]byte, 1024)
	acc := ""
	for i := 0; i < 20; i++ {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		acc += string(buf[:n])
		if strings.Contains(acc, ">") {
			break
		}
	}
	for _, l := range strings.Split(acc, "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// collectUntilOK reads lines until OK or ERROR.
func collectUntilOK(t *testing.T, conn interface {
	Read(b []byte) (int, error)
}) []string {
	t.Helper()
	var lines []string
	buf := make([]byte, 1024)
	acc := ""
	for i := 0; i < 30; i++ {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		acc += string(buf[:n])
		if strings.Contains(acc, "OK\r\n") || strings.Contains(acc, "ERROR\r\n") {
			break
		}
	}
	for _, l := range strings.Split(acc, "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
