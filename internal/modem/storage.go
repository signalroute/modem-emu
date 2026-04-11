// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"fmt"
	"sync"
)

// SMSSlot is one SIM storage entry.
type SMSSlot struct {
	Index  int
	Status int    // 0=unread, 1=read
	PDUHex string // full SMS-DELIVER PDU hex
	Sender string
	Body   string
}

// SIMStorage manages the fixed-size SMS slot array.
type SIMStorage struct {
	mu       sync.Mutex
	slots    []*SMSSlot // 1-indexed; slots[0] unused
	capacity int
}

func NewSIMStorage(capacity int) *SIMStorage {
	return &SIMStorage{slots: make([]*SMSSlot, capacity+1), capacity: capacity}
}

// Store adds an SMS to the next free slot. Returns (index, nil) or (0, ErrFull).
func (s *SIMStorage) Store(sender, body, pduHex string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 1; i <= s.capacity; i++ {
		if s.slots[i] == nil {
			s.slots[i] = &SMSSlot{Index: i, Status: 0, PDUHex: pduHex, Sender: sender, Body: body}
			return i, nil
		}
	}
	return 0, fmt.Errorf("SIM storage full (%d/%d)", s.capacity, s.capacity)
}

// Read returns the slot (marking it read) or nil.
func (s *SIMStorage) Read(idx int) *SMSSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 1 || idx > s.capacity || s.slots[idx] == nil {
		return nil
	}
	s.slots[idx].Status = 1
	return s.slots[idx]
}

// Delete removes a slot. Returns true if it existed.
func (s *SIMStorage) Delete(idx int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 1 || idx > s.capacity || s.slots[idx] == nil {
		return false
	}
	s.slots[idx] = nil
	return true
}

// DeleteAll clears all slots and returns count deleted.
func (s *SIMStorage) DeleteAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := 1; i <= s.capacity; i++ {
		if s.slots[i] != nil {
			s.slots[i] = nil
			n++
		}
	}
	return n
}

// Count returns (used, total).
func (s *SIMStorage) Count() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	used := 0
	for i := 1; i <= s.capacity; i++ {
		if s.slots[i] != nil {
			used++
		}
	}
	return used, s.capacity
}

// Slots returns all occupied slots sorted by index.
func (s *SIMStorage) Slots() []SMSSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []SMSSlot
	for i := 1; i <= s.capacity; i++ {
		if s.slots[i] != nil {
			out = append(out, *s.slots[i])
		}
	}
	return out
}
