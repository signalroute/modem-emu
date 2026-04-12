// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/signalroute/modem-emu/internal/at"
	"github.com/signalroute/modem-emu/internal/config"
)

// ── State ──────────────────────────────────────────────────────────────────

type State int32

const (
	StateActive    State = 0
	StateBanned    State = 1
	StateOff       State = 2
	StateResetting State = 3
)

func (s State) String() string {
	switch s {
	case StateActive:    return "ACTIVE"
	case StateBanned:   return "BANNED"
	case StateOff:      return "OFF"
	case StateResetting: return "RESETTING"
	default:             return "UNKNOWN"
	}
}

// ── SentSMS ────────────────────────────────────────────────────────────────

type SentSMS struct {
	To   string
	Body string
	PDU  string
	At   time.Time
}

// ── Modem ──────────────────────────────────────────────────────────────────

// Modem implements a single cellular modem AT command state machine.
// It is transport-agnostic: it reads/writes to any io.ReadWriteCloser
// (TCP socket, Unix socket, pipe — anything byte-stream).
// One Modem instance is created per accepted connection, so at 10k modems
// you have 10k goroutines each with its own independent state.
type Modem struct {
	cfg config.ModemConfig
	log *slog.Logger

	state       atomic.Int32
	signalCSQ   atomic.Int32
	regStat     atomic.Int32
	echoEnabled atomic.Int32 // 1=on (default per V.250), 0=off after ATE0

	storage *SIMStorage

	mu       sync.Mutex
	sentMsgs []SentSMS
	mr       int // message reference counter for +CMGS

	// urcCh delivers URCs between command responses.
	urcCh chan string
}

// New creates a Modem from config. Does not start processing.
func New(cfg config.ModemConfig, log *slog.Logger) *Modem {
	m := &Modem{
		cfg:     cfg,
		log:     log.With("iccid", cfg.ICCID),
		storage: NewSIMStorage(cfg.SMSStorageMax),
		urcCh:   make(chan string, 64),
	}
	m.state.Store(int32(StateActive))
	m.signalCSQ.Store(int32(cfg.SignalCSQ))
	m.regStat.Store(int32(cfg.RegStat))
	m.echoEnabled.Store(1)
	return m
}

// ICCID returns this modem's SIM ICCID.
func (m *Modem) ICCID() string { return m.cfg.ICCID }

// GetState returns current state.
func (m *Modem) GetState() State { return State(m.state.Load()) }

// ── Control methods (called from HTTP API) ─────────────────────────────────

// InjectSMS stores an incoming SMS in SIM storage and queues a +CMTI URC.
func (m *Modem) InjectSMS(from, body string) (int, error) {
	if State(m.state.Load()) != StateActive {
		return 0, fmt.Errorf("modem is %s, cannot inject SMS", State(m.state.Load()))
	}
	pduHex, err := at.BuildSMSDeliverPDU(from, body)
	if err != nil {
		return 0, err
	}
	idx, err := m.storage.Store(from, body, pduHex)
	if err != nil {
		return 0, err
	}
	select {
	case m.urcCh <- fmt.Sprintf("+CMTI: \"SM\",%d", idx):
	default:
		m.log.Warn("URC channel full — +CMTI dropped")
	}
	m.log.Info("SMS injected", "from", from, "slot", idx)
	return idx, nil
}

func (m *Modem) SetSignal(csq int) {
	m.signalCSQ.Store(int32(csq))
}

func (m *Modem) SetRegistration(stat int) {
	m.regStat.Store(int32(stat))
	if stat == 3 {
		m.state.Store(int32(StateBanned))
	} else if State(m.state.Load()) == StateBanned && stat != 3 {
		m.state.Store(int32(StateActive))
	}
	select {
	case m.urcCh <- fmt.Sprintf("+CREG: %d", stat):
	default:
	}
}

func (m *Modem) SentMessages() []SentSMS {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SentSMS, len(m.sentMsgs))
	copy(out, m.sentMsgs)
	return out
}

func (m *Modem) ClearSentMessages() {
	m.mu.Lock()
	m.sentMsgs = nil
	m.mu.Unlock()
}

func (m *Modem) StorageCount() (int, int) { return m.storage.Count() }

// ── Session ────────────────────────────────────────────────────────────────

// RunSession drives one connected client (io.ReadWriteCloser) until the
// connection is closed or ctx is cancelled.
//
// This is the key scaling point: each accepted connection calls RunSession
// in its own goroutine. The Modem state is fully independent per instance.
func (m *Modem) RunSession(ctx context.Context, conn io.ReadWriteCloser) {
	m.log.Debug("session started")
	defer m.log.Debug("session ended")
	defer conn.Close()

	// Drain URCs whenever we're between commands.
	commandActive := &atomic.Int32{}
	urcCtx, urcCancel := context.WithCancel(ctx)
	defer urcCancel()

	go func() {
		for {
			select {
			case <-urcCtx.Done():
				return
			case urc := <-m.urcCh:
				// Wait briefly if a command is in-flight to avoid interleaving.
				deadline := time.Now().Add(200 * time.Millisecond)
				for commandActive.Load() == 1 && time.Now().Before(deadline) {
					select {
					case <-urcCtx.Done():
						return
					case <-time.After(5 * time.Millisecond):
					}
				}
				if _, err := conn.Write([]byte("\r\n" + urc + "\r\n")); err != nil {
					m.log.Debug("URC write failed, closing URC goroutine", "err", err)
					return
				}
			}
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Split(scanATLines)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		commandActive.Store(1)
		m.handleLine(ctx, line, scanner, conn)
		commandActive.Store(0)
	}
}

// ── AT command dispatcher ──────────────────────────────────────────────────

func (m *Modem) handleLine(ctx context.Context, line string, sc *bufio.Scanner, w io.Writer) {
	m.log.Debug("← recv", "cmd", line)

	if m.echoEnabled.Load() == 1 {
		w.Write([]byte(line + "\r\n"))
	}
	if d := m.cfg.ResponseDelayMs; d > 0 {
		time.Sleep(time.Duration(d+rand.Intn(d/2+1)) * time.Millisecond)
	}

	upper := strings.ToUpper(strings.TrimSpace(line))

	switch {
	// ── Basic V.250 ────────────────────────────────────────────────────
	case upper == "AT":
		ok(w)
	case upper == "ATE0":
		m.echoEnabled.Store(0); ok(w)
	case upper == "ATE1":
		m.echoEnabled.Store(1); ok(w)
	case upper == "ATZ":
		m.echoEnabled.Store(1)
		m.state.Store(int32(StateActive))
		ok(w)
	case upper == "AT&F":
		ok(w)
	case strings.HasPrefix(upper, "AT+CMGF="):
		ok(w)
	case strings.HasPrefix(upper, "AT+CNMI="):
		ok(w)
	case upper == "AT+CMEE=1" || upper == "AT+CMEE=2":
		ok(w)

	// ── Identity ───────────────────────────────────────────────────────
	case upper == "AT+CCID" || upper == "AT+CCID?" || upper == "AT+ICCID?" || upper == "AT+QCCID":
		respond(w, "+CCID: "+m.cfg.ICCID); ok(w)
	case upper == "AT+CIMI":
		respond(w, m.cfg.IMSI); ok(w)
	case upper == "AT+CGSN":
		respond(w, "358240051111110"); ok(w)
	case upper == "ATI" || upper == "AT+GMM" || upper == "AT+CGMM":
		m.respondProfile(w); ok(w)

	// ── Network ────────────────────────────────────────────────────────
	case upper == "AT+COPS?":
		respond(w, fmt.Sprintf(`+COPS: 0,0,"%s",7`, m.cfg.Operator)); ok(w)
	case upper == "AT+CREG?":
		m.handleCREG(w)
	case upper == "AT+CGREG?":
		respond(w, fmt.Sprintf("+CGREG: 0,%d", m.regStat.Load())); ok(w)
	case upper == "AT+CEREG?":
		respond(w, fmt.Sprintf("+CEREG: 0,%d", m.regStat.Load())); ok(w)
	case upper == "AT+CSQ":
		respond(w, fmt.Sprintf("+CSQ: %d,0", m.signalCSQ.Load())); ok(w)
	case upper == "AT+CPMS?":
		m.handleCPMS(w)
	case strings.HasPrefix(upper, "AT+CPMS="):
		ok(w)

	// ── SIM ────────────────────────────────────────────────────────────
	case upper == "AT+CPIN?":
		respond(w, "+CPIN: READY"); ok(w)

	// ── SMS ────────────────────────────────────────────────────────────
	case strings.HasPrefix(upper, "AT+CMGR="):
		m.handleCMGR(w, upper)
	case strings.HasPrefix(upper, "AT+CMGD="):
		m.handleCMGD(w, upper)
	case strings.HasPrefix(upper, "AT+CMGS="):
		m.handleCMGS(ctx, w, sc, upper)
	case upper == "AT+CMGL=4" || upper == "AT+CMGL=\"ALL\"":
		m.handleCMGL(w)

	// ── Power / radio ──────────────────────────────────────────────────
	case upper == "AT+CFUN=0":
		m.state.Store(int32(StateOff)); ok(w)
	case upper == "AT+CFUN=1":
		m.state.Store(int32(StateActive)); ok(w)
	case upper == "AT+CFUN=1,1":
		m.handleHardReset(ctx, w)
	case upper == "AT+CFUN=4":
		m.state.Store(int32(StateOff)); ok(w)

	default:
		m.log.Debug("unrecognised", "cmd", upper)
		w.Write([]byte("\r\nERROR\r\n"))
	}
}

func (m *Modem) handleCREG(w io.Writer) {
	stat := m.regStat.Load()
	switch m.cfg.Profile {
	case "SIM7600", "EC21":
		respond(w, fmt.Sprintf("+CREG: 2,%d", stat))
	default:
		respond(w, fmt.Sprintf("+CREG: 0,%d", stat))
	}
	ok(w)
}

func (m *Modem) handleCPMS(w io.Writer) {
	used, total := m.storage.Count()
	respond(w, fmt.Sprintf(`+CPMS: "SM",%d,%d,"SM",%d,%d,"SM",%d,%d`,
		used, total, used, total, used, total))
	ok(w)
}

func (m *Modem) handleCMGR(w io.Writer, upper string) {
	var idx int
	fmt.Sscanf(strings.TrimPrefix(upper, "AT+CMGR="), "%d", &idx)
	slot := m.storage.Read(idx)
	if slot == nil {
		w.Write([]byte(fmt.Sprintf("\r\n+CME ERROR: 321\r\n")))
		return
	}
	respond(w, fmt.Sprintf("+CMGR: %d,\"\",%d", slot.Status, len(slot.PDUHex)/2))
	respond(w, slot.PDUHex)
	ok(w)
}

func (m *Modem) handleCMGD(w io.Writer, upper string) {
	body := strings.TrimPrefix(upper, "AT+CMGD=")
	parts := strings.SplitN(body, ",", 2)
	var idx, flag int
	fmt.Sscanf(parts[0], "%d", &idx)
	if len(parts) == 2 {
		fmt.Sscanf(parts[1], "%d", &flag)
	}
	if flag == 4 {
		m.storage.DeleteAll()
	} else {
		m.storage.Delete(idx)
	}
	ok(w)
}

func (m *Modem) handleCMGS(ctx context.Context, w io.Writer, sc *bufio.Scanner, upper string) {
	if State(m.state.Load()) != StateActive {
		w.Write([]byte(fmt.Sprintf("\r\n+CMS ERROR: 310\r\n")))
		return
	}
	// Send the "> " prompt.
	w.Write([]byte("\r\n> \r\n"))

	// Read PDU from next line.
	if !sc.Scan() {
		w.Write([]byte("\r\n+CME ERROR: 302\r\n"))
		return
	}
	pduLine := strings.ToUpper(strings.TrimRight(sc.Text(), "\x1A\r\n "))

	decoded := at.DecodeSMSSubmitPDU(pduLine)

	m.mu.Lock()
	m.mr = (m.mr + 1) % 256
	mr := m.mr
	m.sentMsgs = append(m.sentMsgs, SentSMS{
		To: decoded.To, Body: decoded.Body, PDU: pduLine, At: time.Now(),
	})
	m.mu.Unlock()

	m.log.Info("SMS sent", "to", decoded.To, "mr", mr)
	respond(w, fmt.Sprintf("+CMGS: %d", mr))
	ok(w)
}

func (m *Modem) handleCMGL(w io.Writer) {
	slots := m.storage.Slots()
	if len(slots) == 0 {
		ok(w)
		return
	}
	for _, s := range slots {
		respond(w, fmt.Sprintf("+CMGL: %d,%d,\"\",%d", s.Index, s.Status, len(s.PDUHex)/2))
		respond(w, s.PDUHex)
	}
	ok(w)
}

func (m *Modem) handleHardReset(ctx context.Context, w io.Writer) {
	m.state.Store(int32(StateResetting))
	ok(w)
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		return
	}
	m.echoEnabled.Store(1)
	m.state.Store(int32(StateActive))
}

func (m *Modem) respondProfile(w io.Writer) {
	switch m.cfg.Profile {
	case "SIM800L":
		respond(w, "SIM800 R14.18")
	case "SIM7600":
		respond(w, "SIM7600E-H")
		respond(w, "Revision: SIM7600E-H_V2.0")
	case "EC21":
		respond(w, "Quectel_Ltd")
		respond(w, "EC21")
	default:
		respond(w, "go-modem-emu "+m.cfg.Profile)
	}
}

// ── Write helpers ──────────────────────────────────────────────────────────

func ok(w io.Writer)              { w.Write([]byte("\r\nOK\r\n")) }
func respond(w io.Writer, s string) { w.Write([]byte("\r\n" + s)) }

// ── Custom line scanner ────────────────────────────────────────────────────

// scanATLines splits on \r, \n, or Ctrl-Z (0x1A), tolerating mixed line endings.
func scanATLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i := 0; i < len(data); i++ {
		if data[i] == '\r' || data[i] == '\n' || data[i] == 0x1A {
			if i == 0 {
				return 1, nil, nil
			}
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
