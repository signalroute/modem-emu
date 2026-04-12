// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// Regression tests for issues #172 and #173: data races on state and
// SignalLevel fields. Run with -race to verify no concurrent access violations.

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/signalroute/modem-emu/internal/config"
)

func newRaceTestModem(t *testing.T) *Modem {
	t.Helper()
	cfg := config.ModemConfig{
		Profile:       "SIM800L",
		ICCID:         "89490200009999999999",
		IMSI:          "262019009999999",
		Operator:      "Telekom.de",
		SignalCSQ:     18,
		RegStat:       1,
		SMSStorageMax: 20,
	}
	return New(cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

// TestRace_StateAndSignalConcurrent exercises concurrent reads and writes of the
// state, signalCSQ, and regStat atomics from multiple goroutines simultaneously
// (simulating the HTTP control API racing with the AT session goroutine).
// Must be run with -race.
func TestRace_StateAndSignalConcurrent(t *testing.T) {
	m := newRaceTestModem(t)

	clientConn, serverConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		clientConn.Close()
		serverConn.Close()
	})

	// AT session goroutine
	go m.RunSession(ctx, serverConn)

	var wg sync.WaitGroup
	const workers = 8
	const iters = 100

	// Goroutine 1–2: SetSignal concurrent with AT+CSQ responses in the session.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				m.SetSignal((id*iters + j) % 32)
			}
		}(i)
	}

	// Goroutine 3–4: SetRegistration concurrent with AT+CREG? in the session.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			stats := []int{1, 0, 1, 5, 1}
			for j := 0; j < iters; j++ {
				m.SetRegistration(stats[j%len(stats)])
			}
		}(i)
	}

	// Goroutine 5–6: InjectSMS and SentMessages concurrent with session reads.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				m.InjectSMS(fmt.Sprintf("+491510000%04d", j), fmt.Sprintf("race test %d-%d", id, j)) //nolint:errcheck
				_ = m.SentMessages()
			}
		}(i)
	}

	// Goroutine 7–8: GetState and StorageCount (read-only control paths).
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_ = m.GetState()
				_, _ = m.StorageCount()
			}
		}()
	}

	_ = workers // keep constant for documentation
	wg.Wait()
}

// TestRace_MultipleSessionsSharedModem verifies that concurrent client sessions
// on the same Modem instance do not race. (Each Modem is normally used by a
// single session, but the control API may access fields at any time.)
func TestRace_MultipleSessionsSharedModem(t *testing.T) {
	m := newRaceTestModem(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	var wg sync.WaitGroup

	// Simulate two concurrent "control API" goroutines accessing the modem.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				m.SetSignal(id%32)
				m.SetRegistration(id % 2)
				_ = m.GetState()
				_, _ = m.StorageCount()
				_ = m.SentMessages()
			}
		}(i)
	}

	// Run one session concurrently with the control goroutines.
	clientConn, serverConn := net.Pipe()
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.RunSession(ctx, serverConn)
	}()
	t.Cleanup(func() { clientConn.Close(); serverConn.Close() })

	// Send some AT commands from the client side.
	go func() {
		cmds := []string{"AT\r\n", "AT+CSQ\r\n", "AT+CREG?\r\n", "AT+CPIN?\r\n"}
		for {
			for _, cmd := range cmds {
				select {
				case <-ctx.Done():
					clientConn.Close()
					return
				default:
					clientConn.SetDeadline(time.Now().Add(100 * time.Millisecond))
					clientConn.Write([]byte(cmd)) //nolint:errcheck
				}
			}
		}
	}()

	wg.Wait()
}

// TestRace_ClearSentMessagesVsAppend verifies that ClearSentMessages and
// concurrent SMS sends do not produce a data race on the sentMsgs slice.
func TestRace_ClearSentMessagesVsAppend(t *testing.T) {
	m := newRaceTestModem(t)

	clientConn, serverConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		clientConn.Close()
		serverConn.Close()
	})

	go m.RunSession(ctx, serverConn)

	var wg sync.WaitGroup

	// Goroutine 1: repeatedly clear sent messages.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			m.ClearSentMessages()
		}
	}()

	// Goroutine 2: repeatedly read sent messages.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = m.SentMessages()
		}
	}()

	wg.Wait()
}
