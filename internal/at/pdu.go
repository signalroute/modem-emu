// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package at

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// ── GSM 7-bit character set ───────────────────────────────────────────────

var gsm7Charset = [128]rune{
	'@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
	'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', 0x1B, 'Æ', 'æ', 'ß', 'É',
	' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'Ä', 'Ö', 'Ñ', 'Ü', '§',
	'¿', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'ä', 'ö', 'ñ', 'ü', 'à',
}

var runeToGSM7 map[rune]byte

func init() {
	runeToGSM7 = make(map[rune]byte, 128)
	for i, r := range gsm7Charset {
		if r != 0 {
			runeToGSM7[r] = byte(i)
		}
	}
}

// IsGSM7 reports whether all runes in s are in the GSM7 basic character set.
func IsGSM7(s string) bool {
	for _, r := range s {
		if _, ok := runeToGSM7[r]; !ok {
			return false
		}
	}
	return true
}

func encodeGSM7(s string) []byte {
	var out []byte
	for _, r := range s {
		if b, ok := runeToGSM7[r]; ok {
			out = append(out, b)
		}
	}
	return out
}

func packGSM7(codes []byte) []byte {
	n := len(codes)
	packed := make([]byte, (n*7+7)/8)
	for i, c := range codes {
		bytePos := (i * 7) / 8
		bitPos := uint((i * 7) % 8)
		packed[bytePos] |= c << bitPos
		if bitPos > 1 && bytePos+1 < len(packed) {
			packed[bytePos+1] |= c >> (8 - bitPos)
		}
	}
	return packed
}

// DecodeGSM7 unpacks and converts GSM7-encoded bytes to a UTF-8 string.
func DecodeGSM7(data []byte, nChars int) string {
	out := make([]byte, nChars)
	for i := 0; i < nChars; i++ {
		bytePos := (i * 7) / 8
		bitPos := uint((i * 7) % 8)
		b := (data[bytePos] >> bitPos) & 0x7F
		if bitPos > 1 && bytePos+1 < len(data) {
			b |= (data[bytePos+1] << (8 - bitPos)) & 0x7F
		}
		out[i] = b
	}
	var sb strings.Builder
	for _, c := range out {
		if int(c) < len(gsm7Charset) {
			sb.WriteRune(gsm7Charset[c])
		}
	}
	return sb.String()
}

// DecodeUCS2 converts UCS2 big-endian bytes to UTF-8.
func DecodeUCS2(data []byte) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = binary.BigEndian.Uint16(data[i*2:])
	}
	return string(utf16.Decode(u16))
}

func encodeUCS2(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	out := make([]byte, len(u16)*2)
	for i, u := range u16 {
		binary.BigEndian.PutUint16(out[i*2:], u)
	}
	return out
}

// ── Address encoding ──────────────────────────────────────────────────────

// EncodeBCDAddress encodes an MSISDN to BCD bytes.
func EncodeBCDAddress(msisdn string) (numDigits int, addrType byte, data []byte) {
	addrType = 0x81
	digits := msisdn
	if strings.HasPrefix(digits, "+") {
		addrType = 0x91
		digits = digits[1:]
	}
	numDigits = len(digits)
	if len(digits)%2 != 0 {
		digits += "F"
	}
	data = make([]byte, len(digits)/2)
	for i := range data {
		lo := digits[i*2] - '0'
		hi := digits[i*2+1]
		if hi == 'F' {
			hi = 0x0F
		} else {
			hi -= '0'
		}
		data[i] = lo | (hi << 4)
	}
	return
}

// DecodeBCDAddress decodes a BCD-encoded address.
func DecodeBCDAddress(data []byte, numDigits int, addrType byte) string {
	var sb strings.Builder
	if addrType == 0x91 {
		sb.WriteByte('+')
	}
	for i := 0; i < numDigits; i++ {
		b := data[i/2]
		var nibble byte
		if i%2 == 0 {
			nibble = b & 0x0F
		} else {
			nibble = (b >> 4) & 0x0F
		}
		if nibble == 0x0F {
			break
		}
		sb.WriteByte('0' + nibble)
	}
	return sb.String()
}

// ── SCTS ──────────────────────────────────────────────────────────────────

func encodeSCTS(t time.Time) []byte {
	t = t.UTC()
	swap := func(b byte) byte { return (b>>4)&0x0F | (b&0x0F)<<4 }
	enc := func(v int) byte { return swap(byte(v/10*16 + v%10)) }
	return []byte{
		enc(t.Year() % 100),
		enc(int(t.Month())),
		enc(t.Day()),
		enc(t.Hour()),
		enc(t.Minute()),
		enc(t.Second()),
		0x00,
	}
}

// ── PDU builders ──────────────────────────────────────────────────────────

// BuildSMSDeliverPDU builds a complete SMS-DELIVER PDU hex string.
// Includes leading 0x00 SMSC byte (no SMSC) as returned by AT+CMGR.
func BuildSMSDeliverPDU(from, body string) (string, error) {
	var pdu []byte
	pdu = append(pdu, 0x00) // no SMSC
	pdu = append(pdu, 0x04) // SMS-DELIVER, MMS=1

	numDigits, oaType, oaData := EncodeBCDAddress(from)
	pdu = append(pdu, byte(numDigits), oaType)
	pdu = append(pdu, oaData...)
	pdu = append(pdu, 0x00) // PID

	if IsGSM7(body) {
		pdu = append(pdu, 0x00)                // DCS: GSM7
		pdu = append(pdu, encodeSCTS(time.Now())...)
		codes := encodeGSM7(body)
		pdu = append(pdu, byte(len(codes)))
		pdu = append(pdu, packGSM7(codes)...)
	} else {
		pdu = append(pdu, 0x08)                // DCS: UCS2
		pdu = append(pdu, encodeSCTS(time.Now())...)
		ucs2 := encodeUCS2(body)
		pdu = append(pdu, byte(len(ucs2)))
		pdu = append(pdu, ucs2...)
	}

	return strings.ToUpper(fmt.Sprintf("%x", pdu)), nil
}

// PDUHash computes sha256:<hex> over the uppercase PDU hex string.
// This matches the gateway's dedup logic exactly.
func PDUHash(pduHex string) string {
	upper := strings.ToUpper(strings.TrimSpace(pduHex))
	sum := sha256.Sum256([]byte(upper))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ── SMS-SUBMIT PDU decoder ────────────────────────────────────────────────

// DecodedSubmit holds the fields parsed from an outbound SMS-SUBMIT PDU.
type DecodedSubmit struct {
	To   string
	Body string
}

// DecodeSMSSubmitPDU extracts the destination and body from an SMS-SUBMIT PDU hex.
// This is what the emulator calls when AT+CMGS receives a PDU from the gateway.
func DecodeSMSSubmitPDU(hexStr string) DecodedSubmit {
	raw, err := hex.DecodeString(hexStr)
	if err != nil || len(raw) < 10 {
		return DecodedSubmit{To: "unknown"}
	}
	defer func() { recover() }() // guard against malformed PDU

	pos := 0

	// SMSC length (skip)
	smscLen := int(raw[pos])
	pos += 1 + smscLen
	if pos >= len(raw) { return DecodedSubmit{To: "unknown"} }

	// PDU type (skip)
	pos++

	// MR (skip)
	if pos >= len(raw) { return DecodedSubmit{To: "unknown"} }
	pos++

	// DA
	if pos+2 > len(raw) { return DecodedSubmit{To: "unknown"} }
	daLen := int(raw[pos]); pos++
	daType := raw[pos]; pos++
	daBytes := (daLen + 1) / 2
	if pos+daBytes > len(raw) { return DecodedSubmit{To: "unknown"} }
	to := DecodeBCDAddress(raw[pos:pos+daBytes], daLen, daType)
	pos += daBytes

	// PID
	pos++

	// DCS
	if pos >= len(raw) { return DecodedSubmit{To: to} }
	dcs := raw[pos]; pos++

	// VP (1 byte relative — our emitter always uses VPF=10)
	if pos >= len(raw) { return DecodedSubmit{To: to} }
	pos++

	// UDL + UD
	if pos >= len(raw) { return DecodedSubmit{To: to} }
	udl := int(raw[pos]); pos++
	ud := raw[pos:]

	var body string
	switch (dcs >> 2) & 0x03 {
	case 0x00: body = DecodeGSM7(ud, udl)
	case 0x02:
		if udl*2 <= len(ud) {
			body = DecodeUCS2(ud[:udl*2])
		}
	default:
		body = fmt.Sprintf("<binary %d bytes>", udl)
	}
	return DecodedSubmit{To: to, Body: body}
}
