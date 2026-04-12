// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// Table-driven tests for the extended AT commands added in feat/at-commands-extended:
//   AT+COPS?, AT+CSCA?, AT+CPMS?, AT+CNMI?, AT+CLCK="SC",2

import (
	"strings"
	"testing"

	"github.com/signalroute/modem-emu/internal/config"
)

func TestAT_COPS_Query(t *testing.T) {
	_, conn := newTestModemWithCfg(t, config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         "89490200001234567890",
		IMSI:          "262019000012345",
		Operator:      "SignalNet",
		SignalCSQ:     18,
		RegStat:       1,
		SMSStorageMax: 10,
	})
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+COPS?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+COPS?: unexpected result: %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+COPS:") && strings.Contains(l, "SignalNet") {
			found = true
		}
	}
	if !found {
		t.Errorf("+COPS: line with operator not found in: %v", lines)
	}
}

func TestAT_CSCA_Query(t *testing.T) {
	_, conn := testModem(t)
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CSCA?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CSCA?: unexpected result: %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CSCA:") && strings.Contains(l, "+4912345678") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CSCA: line not found in: %v", lines)
	}
}

func TestAT_CPMS_Query(t *testing.T) {
	_, conn := testModem(t)
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CPMS?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CPMS?: unexpected result: %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CPMS:") && strings.Contains(l, `"SM"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("+CPMS: line not found in: %v", lines)
	}
}

func TestAT_CNMI_Query(t *testing.T) {
	_, conn := testModem(t)
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, "AT+CNMI?")
	if lastLine(lines) != "OK" {
		t.Fatalf("AT+CNMI?: unexpected result: %v", lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CNMI:") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CNMI: line not found in: %v", lines)
	}
}

func TestAT_CLCK_SimPinQuery(t *testing.T) {
	_, conn := testModem(t)
	sendCmd(t, conn, "ATE0")

	lines := sendCmd(t, conn, `AT+CLCK="SC",2`)
	if lastLine(lines) != "OK" {
		t.Fatalf(`AT+CLCK="SC",2: unexpected result: %v`, lines)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "+CLCK:") && strings.Contains(l, "0") {
			found = true
		}
	}
	if !found {
		t.Errorf("+CLCK: 0 not found in: %v", lines)
	}
}
