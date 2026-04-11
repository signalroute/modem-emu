// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"os"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/signalroute/modem-emu/internal/config"
)

// ── Test helpers ──────────────────────────────────────────────────────────

func testModem(t *testing.T) (*Modem, net.Conn) {
	t.Helper()
	cfg := config.ModemConfig{
		Profile:         "SIM800L",
		ICCID:           "89490200001234567890",
		IMSI:            "262019000012345",
		Operator:        "Telekom.de",
		SignalCSQ:       18,
		RegStat:         1,
		SMSStorageMax:   10,
		ResponseDelayMs: 0, // no delay in tests
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := New(cfg, log)

	// net.Pipe gives us a synchronous in-process bidirectional connection.
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

// sendCmd writes a command and reads all response lines until OK or ERROR.
func sendCmd(t *testing.T, conn net.Conn, cmd string) []string {
	t.Helper()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	conn.Write([]byte(cmd + "\r\n"))

	var lines []string
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if line == "OK" || line == "ERROR" || strings.HasPrefix(line, "+CME ERROR") || strings.HasPrefix(line, "+CMS ERROR") {
			return lines
		}
	}
	return lines
}

// lastLine returns the final non-empty line (the status).
func lastLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] != "" {
			return lines[i]
		}
	}
	return ""
}

// ── Basic V.250 ───────────────────────────────────────────────────────────

func TestAT_Ping(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT")
	if lastLine(lines) != "OK" {
		t.Errorf("AT: got %v", lines)
	}
}

func TestAT_EchoDefault(t *testing.T) {
	_, conn := testModem(t)
	// Default echo is ON — the command itself is echoed first.
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	conn.Write([]byte("AT\r\n"))
	sc := bufio.NewScanner(conn)
	found := false
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "AT" {
			found = true
			break
		}
		if strings.TrimSpace(sc.Text()) == "OK" {
			break
		}
	}
	if !found {
		t.Error("echo not received for default ATE1 state")
	}
}

func TestAT_EchoDisable(t *testing.T) {
	_, conn := testModem(t)
	// Disable echo.
	sendCmd(t, conn, "ATE0")

	// Next command should NOT be echoed.
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	conn.Write([]byte("AT\r\n"))
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "AT" {
			t.Error("command was echoed after ATE0")
			return
		}
		if line == "OK" {
			return
		}
	}
}

func TestATZ_Reset(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "ATZ")
	if lastLine(lines) != "OK" {
		t.Errorf("ATZ: %v", lines)
	}
}

// ── Identity ──────────────────────────────────────────────────────────────

func TestAT_CCID(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+CCID?")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "89490200001234567890") {
			found = true
		}
	}
	if !found {
		t.Errorf("ICCID not found in response: %v", lines)
	}
	if lastLine(lines) != "OK" {
		t.Errorf("AT+CCID?: got %v", lines)
	}
}

func TestAT_CIMI(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+CIMI")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "262019000012345") {
			found = true
		}
	}
	if !found {
		t.Errorf("IMSI not found: %v", lines)
	}
}

func TestAT_CPIN(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+CPIN?")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "READY") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CPIN READY not found: %v", lines)
	}
}

// ── Network ───────────────────────────────────────────────────────────────

func TestAT_COPS(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+COPS?")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Telekom.de") {
			found = true
		}
	}
	if !found {
		t.Errorf("operator not found in +COPS: %v", lines)
	}
}

func TestAT_CREG_Home(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+CREG?")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CREG:") && strings.Contains(l, "1") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CREG: 1 (home) not found: %v", lines)
	}
}

func TestAT_CSQ(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+CSQ")
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "+CSQ:") {
			found = true
			// Must contain 18 (the configured signal).
			if !strings.Contains(l, "18") {
				t.Errorf("+CSQ: expected 18, got %q", l)
			}
		}
	}
	if !found {
		t.Errorf("+CSQ not found: %v", lines)
	}
}

func TestAT_CSQ_AfterSetSignal(t *testing.T) {
	m, conn := testModem(t)
	m.SetSignal(5)
	lines := sendCmd(t, conn, "AT+CSQ")
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "+CSQ:") && strings.Contains(l, "5") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CSQ did not reflect SetSignal(5): %v", lines)
	}
}

func TestAT_CREG_AfterBan(t *testing.T) {
	m, conn := testModem(t)
	m.SetRegistration(3) // denied
	lines := sendCmd(t, conn, "AT+CREG?")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CREG:") && strings.Contains(l, "3") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CREG: 3 not reflected after SetRegistration(3): %v", lines)
	}
	if m.GetState() != StateBanned {
		t.Errorf("state: got %s, want BANNED", m.GetState())
	}
}

// ── CPMS / storage ────────────────────────────────────────────────────────

func TestAT_CPMS_Empty(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+CPMS?")
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "+CPMS:") {
			found = true
			// Should contain 0,10 (0 used, 10 total).
			if !strings.Contains(l, "0,10") {
				t.Errorf("+CPMS: expected 0/10, got %q", l)
			}
		}
	}
	if !found {
		t.Errorf("+CPMS not found: %v", lines)
	}
}

func TestAT_CPMS_AfterInject(t *testing.T) {
	m, conn := testModem(t)
	m.InjectSMS("+491234", "test message")

	lines := sendCmd(t, conn, "AT+CPMS?")
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "+CPMS:") {
			found = true
			// Should now contain 1,10.
			if !strings.Contains(l, "1,10") {
				t.Errorf("+CPMS: expected 1/10, got %q", l)
			}
		}
	}
	if !found {
		t.Errorf("+CPMS not found: %v", lines)
	}
}

// ── SMS: CMGR + CMGD ─────────────────────────────────────────────────────

func TestAT_CMGR_ValidSlot(t *testing.T) {
	m, conn := testModem(t)
	idx, err := m.InjectSMS("+4915198765432", "Your OTP is 391827")
	if err != nil {
		t.Fatalf("InjectSMS: %v", err)
	}

	lines := sendCmd(t, conn, "AT+CMGR="+itoa(idx))
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "+CMGR:") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CMGR response not found: %v", lines)
	}
	if lastLine(lines) != "OK" {
		t.Errorf("CMGR not OK: %v", lines)
	}
}

func TestAT_CMGR_EmptySlot(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+CMGR=5")
	last := lastLine(lines)
	if !strings.HasPrefix(last, "+CME ERROR") {
		t.Errorf("CMGR on empty slot: got %v, want +CME ERROR", lines)
	}
}

func TestAT_CMGD_Delete(t *testing.T) {
	m, conn := testModem(t)
	idx, _ := m.InjectSMS("+491234", "delete me")

	lines := sendCmd(t, conn, "AT+CMGD="+itoa(idx))
	if lastLine(lines) != "OK" {
		t.Errorf("CMGD: %v", lines)
	}
	// Slot should now be empty.
	used, _ := m.StorageCount()
	if used != 0 {
		t.Errorf("storage still has %d messages after CMGD", used)
	}
}

func TestAT_CMGD_DeleteAll(t *testing.T) {
	m, conn := testModem(t)
	m.InjectSMS("+491", "a")
	m.InjectSMS("+492", "b")
	m.InjectSMS("+493", "c")

	lines := sendCmd(t, conn, "AT+CMGD=1,4")
	if lastLine(lines) != "OK" {
		t.Errorf("CMGD=1,4: %v", lines)
	}
	used, _ := m.StorageCount()
	if used != 0 {
		t.Errorf("storage has %d messages after delete-all", used)
	}
}

// ── InjectSMS + +CMTI URC ────────────────────────────────────────────────

func TestInjectSMS_FiresCMTI(t *testing.T) {
	m, conn := testModem(t)

	// Drain the connection in background, capture URCs.
	urcCh := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "+CMTI:") {
				urcCh <- line
			}
		}
	}()

	m.InjectSMS("+4915198765432", "Your OTP is 123456")

	select {
	case urc := <-urcCh:
		if !strings.Contains(urc, "+CMTI:") {
			t.Errorf("unexpected URC: %q", urc)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for +CMTI URC")
	}
}

func TestInjectSMS_StorageFull(t *testing.T) {
	m, _ := testModem(t)
	// Fill all 10 slots.
	for i := 0; i < 10; i++ {
		if _, err := m.InjectSMS("+491", "msg"); err != nil {
			t.Fatalf("unexpected error at slot %d: %v", i, err)
		}
	}
	// Next must fail.
	_, err := m.InjectSMS("+491", "overflow")
	if err == nil {
		t.Error("expected error when SIM storage is full")
	}
}

// ── Power control ─────────────────────────────────────────────────────────

func TestAT_CFUN0_ThenCFUN1(t *testing.T) {
	m, conn := testModem(t)

	lines := sendCmd(t, conn, "AT+CFUN=0")
	if lastLine(lines) != "OK" {
		t.Errorf("CFUN=0: %v", lines)
	}
	if m.GetState() != StateOff {
		t.Errorf("state after CFUN=0: %s", m.GetState())
	}

	lines = sendCmd(t, conn, "AT+CFUN=1")
	if lastLine(lines) != "OK" {
		t.Errorf("CFUN=1: %v", lines)
	}
	if m.GetState() != StateActive {
		t.Errorf("state after CFUN=1: %s", m.GetState())
	}
}

// ── Unknown command ───────────────────────────────────────────────────────

func TestAT_UnknownCommand(t *testing.T) {
	_, conn := testModem(t)
	lines := sendCmd(t, conn, "AT+FAKECMD")
	if lastLine(lines) != "ERROR" {
		t.Errorf("unknown cmd: got %v, want ERROR", lines)
	}
}

// ── SentMessages ─────────────────────────────────────────────────────────

func TestClearSentMessages(t *testing.T) {
	m, _ := testModem(t)
	// Synthesise a sent record directly.
	m.mu.Lock()
	m.sentMsgs = append(m.sentMsgs, SentSMS{To: "+491", Body: "test"})
	m.mu.Unlock()

	if len(m.SentMessages()) != 1 {
		t.Fatal("expected 1 sent message")
	}
	m.ClearSentMessages()
	if len(m.SentMessages()) != 0 {
		t.Error("expected 0 after clear")
	}
}

// ── Helper ────────────────────────────────────────────────────────────────

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
