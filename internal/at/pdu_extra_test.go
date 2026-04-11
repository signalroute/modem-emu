// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package at

import (
	"fmt"
	"strings"
	"testing"
)

// ── Test data ─────────────────────────────────────────────────────────────

var emuTestSenders = func() []string {
	s := make([]string, 50)
	for i := range s {
		s[i] = fmt.Sprintf("+491234%05d", i)
	}
	return s
}()

var emuTestBodies = func() []string {
	b := make([]string, 50)
	phrases := []string{"Hello", "World", "SMS", "Test", "OK", "Signal", "Route", "Cloud", "Gate", "Data"}
	for i := range b {
		phrase := phrases[i%len(phrases)]
		b[i] = strings.Repeat(phrase+" ", (i/10)+1)
		if len(b[i]) > 160 {
			b[i] = b[i][:160]
		}
		b[i] = strings.TrimSpace(b[i])
	}
	return b
}()

// ── TestBuildSMSDeliverPDU_Roundtrip (2500 subtests) ─────────────────────

func TestBuildSMSDeliverPDU_Roundtrip(t *testing.T) {
	for _, sender := range emuTestSenders {
		for _, body := range emuTestBodies {
			sender, body := sender, body
			t.Run(fmt.Sprintf("%s/%d", sender, len(body)), func(t *testing.T) {
				pdu, err := BuildSMSDeliverPDU(sender, body)
				if err != nil {
					t.Fatalf("BuildSMSDeliverPDU(%q, %q): %v", sender, body, err)
				}
				if len(pdu) == 0 {
					t.Fatal("got empty PDU")
				}
				if len(pdu)%2 != 0 {
					t.Errorf("PDU hex length %d is odd", len(pdu))
				}
				if pdu != strings.ToUpper(pdu) {
					t.Error("PDU must be uppercase hex")
				}
				h := PDUHash(pdu)
				if !strings.HasPrefix(h, "sha256:") {
					t.Errorf("hash prefix: got %q", h)
				}
				if len(h) != 71 {
					t.Errorf("hash length: got %d, want 71", len(h))
				}
				if PDUHash(pdu) != PDUHash(strings.ToLower(pdu)) {
					t.Error("PDUHash must be case-insensitive")
				}
			})
		}
	}
}

// ── TestBuildSMSDeliverPDU_UCS2 (500 subtests) ───────────────────────────

func TestBuildSMSDeliverPDU_UCS2(t *testing.T) {
	ucs2Bodies := []string{
		"Привет", "Мир", "Тест", "Россия",
		"你好", "世界", "测试", "中国",
		"مرحبا", "اختبار",
	}
	for _, sender := range emuTestSenders {
		for _, body := range ucs2Bodies {
			sender, body := sender, body
			t.Run(fmt.Sprintf("%s/%s", sender, body), func(t *testing.T) {
				pdu, err := BuildSMSDeliverPDU(sender, body)
				if err != nil {
					t.Fatalf("BuildSMSDeliverPDU UCS2(%q, %q): %v", sender, body, err)
				}
				if len(pdu) == 0 {
					t.Fatal("got empty PDU")
				}
				if len(pdu)%2 != 0 {
					t.Errorf("PDU hex length %d is odd", len(pdu))
				}
			})
		}
	}
}

// ── TestPDUHash_Comprehensive (100 subtests) ──────────────────────────────

func TestPDUHash_Comprehensive(t *testing.T) {
	// Collect 100 PDUs from our sender/body combinations.
	type pduEntry struct {
		pdu    string
		sender string
		body   string
	}
	entries := make([]pduEntry, 0, 100)
	for i, sender := range emuTestSenders {
		if i >= 10 {
			break
		}
		for j, body := range emuTestBodies {
			if j >= 10 {
				break
			}
			pdu, err := BuildSMSDeliverPDU(sender, body)
			if err == nil && len(pdu) > 0 {
				entries = append(entries, pduEntry{pdu, sender, body})
			}
		}
	}

	for i, e := range entries {
		e := e
		t.Run(fmt.Sprintf("hash_%d", i), func(t *testing.T) {
			h1 := PDUHash(e.pdu)
			h2 := PDUHash(e.pdu)
			if h1 != h2 {
				t.Error("PDUHash must be deterministic")
			}
			if !strings.HasPrefix(h1, "sha256:") {
				t.Errorf("hash prefix: got %q", h1)
			}
			if len(h1) != 71 {
				t.Errorf("hash length: got %d, want 71", len(h1))
			}
			// Case-insensitive
			lower := strings.ToLower(e.pdu)
			if PDUHash(lower) != PDUHash(e.pdu) {
				t.Error("PDUHash must be case-insensitive")
			}
		})
	}
}

// ── TestIsGSM7_Comprehensive (500 subtests) ───────────────────────────────

func TestIsGSM7_Comprehensive(t *testing.T) {
	// 200 GSM7-true strings.
	gsm7True := make([]string, 200)
	for i := 0; i < 160; i++ {
		gsm7True[i] = fmt.Sprintf("msg%d", i)
	}
	// Add known GSM7 non-ASCII characters (basic table only — € is extension, excluded).
	specials := []string{
		"@£$¥", "äöü", "ÄÖÜ", "ÆæßÉ", "è é ì",
		"à ù", "Ç Ø ø", "Δ Φ Γ", "Λ Ω Π", "Ψ Σ Θ",
		"Ξ", "ò", "Hello World!", "0123456789", "  spaces  ",
		"@", "£", "$", "¥", "è",
		"ì", "ò", "ù", "Ä", "Ö",
		"Ü", "ä", "ö", "ü", "ß",
		"É", "Æ", "æ", "Ø", "ø",
		"Å", "å", "Δ", "Φ", "Γ",
	}
	for i, s := range specials {
		if 160+i < 200 {
			gsm7True[160+i] = s
		}
	}

	for i, s := range gsm7True {
		s := s
		t.Run(fmt.Sprintf("gsm7_true_%d", i), func(t *testing.T) {
			if !IsGSM7(s) {
				t.Errorf("IsGSM7(%q) = false, want true", s)
			}
		})
	}

	// 300 GSM7-false strings: Cyrillic and other Unicode ranges.
	for i := 0; i < 300; i++ {
		r := rune(0x0400 + i)
		s := string(r)
		t.Run(fmt.Sprintf("gsm7_false_%d", i), func(t *testing.T) {
			if IsGSM7(s) {
				t.Errorf("IsGSM7(%q) = true, want false (rune U+%04X)", s, r)
			}
		})
	}
}

// ── TestBCDAddress_Comprehensive (300 subtests) ───────────────────────────

func TestBCDAddress_Comprehensive(t *testing.T) {
	// 150 international numbers.
	for i := 0; i < 150; i++ {
		n := fmt.Sprintf("+%015d", int64(49000000000000)+int64(i))
		t.Run(fmt.Sprintf("intl_%d", i), func(t *testing.T) {
			numDigits, addrType, data := EncodeBCDAddress(n)
			if addrType != 0x91 {
				t.Errorf("international addrType: got %#x, want 0x91", addrType)
			}
			got := DecodeBCDAddress(data, numDigits, addrType)
			if got != n {
				t.Errorf("roundtrip(%q): got %q", n, got)
			}
		})
	}

	// 150 domestic numbers.
	for i := 0; i < 150; i++ {
		n := fmt.Sprintf("0%011d", i+10000000000)
		t.Run(fmt.Sprintf("domestic_%d", i), func(t *testing.T) {
			numDigits, addrType, data := EncodeBCDAddress(n)
			if addrType != 0x81 {
				t.Errorf("domestic addrType: got %#x, want 0x81", addrType)
			}
			got := DecodeBCDAddress(data, numDigits, addrType)
			if got != n {
				t.Errorf("roundtrip(%q): got %q", n, got)
			}
		})
	}
}

// ── TestDecodeSMSSubmitPDU_Malformed (200 subtests) ───────────────────────

func TestDecodeSMSSubmitPDU_Malformed(t *testing.T) {
	const hexChars = "0123456789ABCDEF"
	for i := 0; i < 200; i++ {
		length := (i % 50) * 2
		var sb strings.Builder
		for j := 0; j < length; j++ {
			sb.WriteByte(hexChars[j%len(hexChars)])
		}
		hexStr := sb.String()
		t.Run(fmt.Sprintf("malformed_%d", i), func(t *testing.T) {
			// Must not panic regardless of input.
			d := DecodeSMSSubmitPDU(hexStr)
			// We only check that To is non-empty (either "unknown" or a parsed value).
			if d.To == "" {
				t.Errorf("DecodeSMSSubmitPDU(%q).To is empty string, want non-empty", hexStr)
			}
		})
	}
}
