// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package at

import (
	"strings"
	"testing"
)

// ── IsGSM7 ────────────────────────────────────────────────────────────────

func TestIsGSM7(t *testing.T) {
	cases := []struct{ s string; want bool }{
		{"Hello World", true},
		{"Your OTP is 882731.", true},
		{"@£$¥", true},
		{"äöüÄÖÜ", true},
		{"Привет", false},
		{"中文", false},
		{"Hello 🌍", false},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			if got := IsGSM7(tc.s); got != tc.want {
				t.Errorf("IsGSM7(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

// ── BuildSMSDeliverPDU + PDUHash ──────────────────────────────────────────

func TestBuildSMSDeliverPDU_Valid(t *testing.T) {
	pdu, err := BuildSMSDeliverPDU("+4915198765432", "Your OTP is 391827")
	if err != nil {
		t.Fatalf("BuildSMSDeliverPDU: %v", err)
	}
	// Must be uppercase hex.
	if pdu != strings.ToUpper(pdu) {
		t.Error("PDU must be uppercase hex")
	}
	// Must be even-length hex string.
	if len(pdu)%2 != 0 {
		t.Errorf("PDU hex length %d is odd", len(pdu))
	}
	// First byte must be 0x00 (no SMSC).
	if pdu[:2] != "00" {
		t.Errorf("PDU[0] = %q, want 00 (no SMSC)", pdu[:2])
	}
}

func TestBuildSMSDeliverPDU_UCS2Body(t *testing.T) {
	pdu, err := BuildSMSDeliverPDU("+491234", "Привет мир")
	if err != nil {
		t.Fatalf("BuildSMSDeliverPDU UCS2: %v", err)
	}
	if len(pdu) == 0 {
		t.Error("empty PDU for UCS2 body")
	}
}

func TestPDUHash_Format(t *testing.T) {
	pdu, _ := BuildSMSDeliverPDU("+491234", "test")
	h := PDUHash(pdu)
	if !strings.HasPrefix(h, "sha256:") {
		t.Errorf("hash prefix: got %q", h[:7])
	}
	if len(h) != len("sha256:")+64 {
		t.Errorf("hash length: got %d", len(h))
	}
}

func TestPDUHash_Deterministic(t *testing.T) {
	pdu, _ := BuildSMSDeliverPDU("+491234", "hello")
	h1 := PDUHash(pdu)
	h2 := PDUHash(pdu)
	if h1 != h2 {
		t.Error("PDUHash must be deterministic")
	}
}

func TestPDUHash_CaseInsensitive(t *testing.T) {
	pdu, _ := BuildSMSDeliverPDU("+491234", "hello")
	lower := strings.ToLower(pdu)
	upper := strings.ToUpper(pdu)
	if PDUHash(lower) != PDUHash(upper) {
		t.Error("PDUHash must be case-insensitive (normalises to upper before hashing)")
	}
}

func TestPDUHash_Different(t *testing.T) {
	p1, _ := BuildSMSDeliverPDU("+491234", "msg A")
	p2, _ := BuildSMSDeliverPDU("+491234", "msg B")
	if PDUHash(p1) == PDUHash(p2) {
		t.Error("different bodies must produce different hashes")
	}
}

// ── EncodeBCDAddress + DecodeBCDAddress ──────────────────────────────────

func TestBCDAddress_Roundtrip_International(t *testing.T) {
	numbers := []string{
		"+4917629900000",
		"+49151123456789",
		"+1234567890",
	}
	for _, n := range numbers {
		t.Run(n, func(t *testing.T) {
			numDigits, addrType, data := EncodeBCDAddress(n)
			if addrType != 0x91 {
				t.Errorf("addrType: got %#x, want 0x91", addrType)
			}
			got := DecodeBCDAddress(data, numDigits, addrType)
			if got != n {
				t.Errorf("roundtrip(%q): got %q", n, got)
			}
		})
	}
}

func TestBCDAddress_Domestic(t *testing.T) {
	numDigits, addrType, data := EncodeBCDAddress("0151123456")
	if addrType != 0x81 {
		t.Errorf("domestic: addrType=%#x, want 0x81", addrType)
	}
	got := DecodeBCDAddress(data, numDigits, addrType)
	if got != "0151123456" {
		t.Errorf("roundtrip: got %q", got)
	}
}

// ── DecodeSMSSubmitPDU ────────────────────────────────────────────────────

func TestDecodeSMSSubmitPDU_GSM7(t *testing.T) {
	// Build a PDU by encoding a SUBMIT through BuildSMSDeliverPDU (for the
	// FROM side) — then verify DecodeSMSSubmitPDU can parse a SUBMIT PDU
	// by constructing one manually.
	// For the emulator's purposes we test that malformed input doesn't panic.
	cases := []struct {
		hex  string
		wantTo string
	}{
		{"", "unknown"},
		{"00", "unknown"},
		{"ZZZZ", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.hex[:min(8, len(tc.hex))]+"_malformed", func(t *testing.T) {
			d := DecodeSMSSubmitPDU(tc.hex)
			if d.To != tc.wantTo {
				t.Errorf("To: got %q, want %q", d.To, tc.wantTo)
			}
		})
	}
}

func min(a, b int) int {
	if a < b { return a }
	return b
}

// ── DecodeGSM7 / DecodeUCS2 ───────────────────────────────────────────────

func TestDecodeGSM7_Roundtrip(t *testing.T) {
	msgs := []string{"Hi", "Hello World", "0123456789", "Test@"}
	for _, msg := range msgs {
		t.Run(msg, func(t *testing.T) {
			// Encode via the private helpers (accessible in same package).
			codes := encodeGSM7(msg)
			packed := packGSM7(codes)
			got := DecodeGSM7(packed, len(codes))
			if got != msg {
				t.Errorf("GSM7 roundtrip(%q): got %q", msg, got)
			}
		})
	}
}

func TestDecodeUCS2_Roundtrip(t *testing.T) {
	msgs := []string{"Привет", "中文", "Hello 🌍"}
	for _, msg := range msgs {
		t.Run(msg, func(t *testing.T) {
			encoded := encodeUCS2(msg)
			got := DecodeUCS2(encoded)
			if got != msg {
				t.Errorf("UCS2 roundtrip(%q): got %q", msg, got)
			}
		})
	}
}
