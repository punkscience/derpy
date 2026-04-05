//go:build windows

package main

// mpris_windows.go provides no-op stubs for the MPRISService on Windows.
// MPRIS2 is a Linux/D-Bus standard; on Windows the service simply does nothing.

import tea "github.com/charmbracelet/bubbletea"

// MPRISService is an inert stub on Windows.
type MPRISService struct{}

// NewMPRISService returns an empty service.  On Windows there is no D-Bus, so
// MPRIS2 control is unavailable.
func NewMPRISService(_ *tea.Program) (*MPRISService, error) {
	return &MPRISService{}, nil
}

// Close is a no-op on Windows.
func (s *MPRISService) Close() {}

// NotifyStateChanged is a no-op on Windows.
func (s *MPRISService) NotifyStateChanged(_ *PlayerModel) {}

// EmitSeeked is a no-op on Windows.
func (s *MPRISService) EmitSeeked(_ *PlayerModel) {}
