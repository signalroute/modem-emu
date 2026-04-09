// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package control

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/signalroute/go-modem-emu/internal/at"
	"github.com/signalroute/go-modem-emu/internal/modem"
	"github.com/signalroute/go-modem-emu/internal/mux"
)

// Server is the HTTP control/injection API.
type Server struct {
	pool *mux.Pool
	log  *slog.Logger
}

func NewServer(pool *mux.Pool, log *slog.Logger) *Server {
	return &Server{pool: pool, log: log.With("component", "control")}
}

// Handler returns the chi router with all control endpoints.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	// ── Global status ─────────────────────────────────────────────────
	r.Get("/modems", s.listModems)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})

	// ── Per-modem ─────────────────────────────────────────────────────
	r.Route("/modems/{iccid}", func(r chi.Router) {
		r.Get("/", s.getModem)

		// Inject one incoming SMS (fires +CMTI URC).
		r.Post("/sms/inject", s.injectSMS)

		// List all SMS the gateway sent through this modem.
		r.Get("/sms/sent", s.listSentSMS)

		// Clear sent SMS history (reset between test runs).
		r.Delete("/sms/sent", s.clearSentSMS)

		// Signal quality control.
		r.Put("/signal", s.setSignal)

		// Registration state control.
		r.Put("/registration", s.setRegistration)

		// SIM storage status.
		r.Get("/storage", s.getStorage)
	})

	// ── Bulk / scenario endpoints ──────────────────────────────────────

	// Ban a SIM: sets +CREG: 3 so gateway detects SIM_BANNED.
	// ?iccid=... (omit to target modem 0)
	r.Post("/scenarios/ban", s.scenarioBan)

	// Restore registration to home: +CREG: 1.
	r.Post("/scenarios/restore", s.scenarioRestore)

	// Flood: inject N SMS rapidly.
	// ?iccid=...&count=N&from=+49...
	r.Post("/scenarios/flood", s.scenarioFlood)

	// Weak signal: drop CSQ to 3 (~-107 dBm).
	r.Post("/scenarios/weak-signal", s.scenarioWeakSignal)

	// Full SIM: inject messages until storage is full.
	r.Post("/scenarios/fill-storage", s.scenarioFillStorage)

	return r
}

// ── Modem list ─────────────────────────────────────────────────────────────

type modemInfo struct {
	Index        int    `json:"index"`
	ICCID        string `json:"iccid"`
	Profile      string `json:"profile"`
	Addr         string `json:"addr"`
	Network      string `json:"network"`
	State        string `json:"state"`
	SentCount    int    `json:"sent_sms_count"`
	StorageUsed  int    `json:"storage_used"`
	StorageTotal int    `json:"storage_total"`
}

func (s *Server) listModems(w http.ResponseWriter, _ *http.Request) {
	var out []modemInfo
	for _, slot := range s.pool.Slots() {
		used, total := slot.Modem.StorageCount()
		out = append(out, modemInfo{
			Index:        slot.Index,
			ICCID:        slot.Modem.ICCID(),
			Profile:      slot.Modem.GetState().String(), // placeholder until Profile() exposed
			Addr:         slot.Addr,
			Network:      slot.Network,
			State:        slot.Modem.GetState().String(),
			SentCount:    len(slot.Modem.SentMessages()),
			StorageUsed:  used,
			StorageTotal: total,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) getModem(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.resolveSlot(w, r)
	if !ok {
		return
	}
	used, total := slot.Modem.StorageCount()
	writeJSON(w, 200, modemInfo{
		Index: slot.Index, ICCID: slot.Modem.ICCID(),
		Addr: slot.Addr, Network: slot.Network,
		State:        slot.Modem.GetState().String(),
		SentCount:    len(slot.Modem.SentMessages()),
		StorageUsed:  used, StorageTotal: total,
	})
}

// ── SMS injection ──────────────────────────────────────────────────────────

type injectReq struct {
	From string `json:"from"` // sender MSISDN (e.g. "+4915198765432") or alphanumeric shortcode
	Body string `json:"body"` // UTF-8 message text
}

type injectResp struct {
	SlotIndex int    `json:"slot_index"`
	PDUHash   string `json:"pdu_hash"`
	Message   string `json:"message"`
}

func (s *Server) injectSMS(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.resolveSlot(w, r)
	if !ok {
		return
	}
	var req injectReq
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.From == "" || req.Body == "" {
		writeError(w, 400, "from and body are required")
		return
	}
	idx, err := slot.Modem.InjectSMS(req.From, req.Body)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	pduHex, _ := at.BuildSMSDeliverPDU(req.From, req.Body)
	s.log.Info("SMS injected", "iccid", slot.Modem.ICCID(), "from", req.From, "slot", idx)
	writeJSON(w, 201, injectResp{
		SlotIndex: idx,
		PDUHash:   at.PDUHash(pduHex),
		Message:   "+CMTI URC queued on socket",
	})
}

// ── Sent SMS ───────────────────────────────────────────────────────────────

type sentItem struct {
	To   string    `json:"to"`
	Body string    `json:"body"`
	PDU  string    `json:"pdu"`
	At   time.Time `json:"at"`
}

func (s *Server) listSentSMS(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.resolveSlot(w, r)
	if !ok {
		return
	}
	msgs := slot.Modem.SentMessages()
	out := make([]sentItem, len(msgs))
	for i, m := range msgs {
		out[i] = sentItem{To: m.To, Body: m.Body, PDU: m.PDU, At: m.At}
	}
	writeJSON(w, 200, out)
}

func (s *Server) clearSentSMS(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.resolveSlot(w, r)
	if !ok {
		return
	}
	slot.Modem.ClearSentMessages()
	writeJSON(w, 200, map[string]string{"status": "cleared"})
}

// ── Signal control ─────────────────────────────────────────────────────────

type signalReq struct {
	CSQ int `json:"csq"` // 0–31 or 99 (unknown)
}

func (s *Server) setSignal(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.resolveSlot(w, r)
	if !ok {
		return
	}
	var req signalReq
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.CSQ < 0 || (req.CSQ > 31 && req.CSQ != 99) {
		writeError(w, 400, "csq must be 0–31 or 99")
		return
	}
	slot.Modem.SetSignal(req.CSQ)
	writeJSON(w, 200, map[string]any{
		"iccid": slot.Modem.ICCID(),
		"csq":   req.CSQ,
		"rssi":  -113 + req.CSQ*2,
	})
}

// ── Registration control ───────────────────────────────────────────────────

type regReq struct {
	Stat int `json:"stat"` // 0=not registered, 1=home, 2=searching, 3=denied, 5=roaming
}

func (s *Server) setRegistration(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.resolveSlot(w, r)
	if !ok {
		return
	}
	var req regReq
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Stat < 0 || req.Stat > 5 {
		writeError(w, 400, "stat must be 0–5")
		return
	}
	slot.Modem.SetRegistration(req.Stat)
	writeJSON(w, 200, map[string]any{"iccid": slot.Modem.ICCID(), "stat": req.Stat})
}

// ── Storage ────────────────────────────────────────────────────────────────

func (s *Server) getStorage(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.resolveSlot(w, r)
	if !ok {
		return
	}
	used, total := slot.Modem.StorageCount()
	pct := 0
	if total > 0 {
		pct = used * 100 / total
	}
	writeJSON(w, 200, map[string]any{
		"iccid": slot.Modem.ICCID(),
		"used":  used, "total": total, "pct": pct,
	})
}

// ── Scenarios ──────────────────────────────────────────────────────────────

func (s *Server) scenarioBan(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.lookupOrFirst(r.URL.Query().Get("iccid"))
	if !ok {
		writeError(w, 404, "modem not found")
		return
	}
	slot.Modem.SetRegistration(3)
	writeJSON(w, 200, map[string]string{
		"iccid":   slot.Modem.ICCID(),
		"status":  "banned",
		"message": "+CREG: 3 pushed — gateway should detect SIM_BANNED",
	})
}

func (s *Server) scenarioRestore(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.lookupOrFirst(r.URL.Query().Get("iccid"))
	if !ok {
		writeError(w, 404, "modem not found")
		return
	}
	slot.Modem.SetRegistration(1)
	writeJSON(w, 200, map[string]string{
		"iccid": slot.Modem.ICCID(), "status": "restored",
	})
}

func (s *Server) scenarioWeakSignal(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.lookupOrFirst(r.URL.Query().Get("iccid"))
	if !ok {
		writeError(w, 404, "modem not found")
		return
	}
	slot.Modem.SetSignal(3) // ~-107 dBm — barely usable
	writeJSON(w, 200, map[string]string{
		"iccid": slot.Modem.ICCID(), "status": "weak_signal", "csq": "3",
	})
}

type floodResp struct {
	Injected int      `json:"injected"`
	Failed   int      `json:"failed"`
	Hashes   []string `json:"pdu_hashes"`
}

func (s *Server) scenarioFlood(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.lookupOrFirst(r.URL.Query().Get("iccid"))
	if !ok {
		writeError(w, 404, "modem not found")
		return
	}
	from := r.URL.Query().Get("from")
	if from == "" {
		from = "+4915100000000"
	}
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 || count > 200 {
		count = 5
	}

	resp := floodResp{}
	for i := 0; i < count; i++ {
		body := fmt.Sprintf("Flood test SMS #%d", i+1)
		if _, err := slot.Modem.InjectSMS(from, body); err != nil {
			resp.Failed++
			continue
		}
		resp.Injected++
		pduHex, _ := at.BuildSMSDeliverPDU(from, body)
		resp.Hashes = append(resp.Hashes, at.PDUHash(pduHex))
	}
	writeJSON(w, 201, resp)
}

func (s *Server) scenarioFillStorage(w http.ResponseWriter, r *http.Request) {
	slot, ok := s.lookupOrFirst(r.URL.Query().Get("iccid"))
	if !ok {
		writeError(w, 404, "modem not found")
		return
	}
	from := "+4915100000099"
	injected := 0
	for {
		body := fmt.Sprintf("Fill test SMS #%d", injected+1)
		if _, err := slot.Modem.InjectSMS(from, body); err != nil {
			break // SIM full
		}
		injected++
	}
	used, total := slot.Modem.StorageCount()
	writeJSON(w, 201, map[string]any{
		"iccid":    slot.Modem.ICCID(),
		"injected": injected,
		"used":     used,
		"total":    total,
		"message":  "SIM storage full — gateway should detect SIM_FULL",
	})
}

// ── Helpers ────────────────────────────────────────────────────────────────

func (s *Server) resolveSlot(w http.ResponseWriter, r *http.Request) (*mux.ModemSlot, bool) {
	iccid := chi.URLParam(r, "iccid")
	slot, ok := s.pool.Lookup(iccid)
	if !ok {
		writeError(w, 404, "modem not found: "+iccid)
	}
	return slot, ok
}

func (s *Server) lookupOrFirst(iccid string) (*mux.ModemSlot, bool) {
	if iccid != "" {
		return s.pool.Lookup(iccid)
	}
	slots := s.pool.Slots()
	if len(slots) == 0 {
		return nil, false
	}
	return slots[0], true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
