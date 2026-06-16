//go:build darwin

package main

// mpris_darwin.go provides no-op stubs for the MPRISService on macOS.
// MPRIS2 is a Linux/D-Bus standard; on macOS the service simply does nothing.

import tea "github.com/charmbracelet/bubbletea"

type MPRISService struct{}

func NewMPRISService(_ *tea.Program) (*MPRISService, error) {
	return &MPRISService{}, nil
}

func (s *MPRISService) Close() {}

func (s *MPRISService) NotifyStateChanged(_ *PlayerModel) {}

func (s *MPRISService) EmitSeeked(_ *PlayerModel) {}
