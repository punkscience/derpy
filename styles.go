package main

// styles.go — Punk Science 2026 brand tokens for the derpy TUI.
//
// All lipgloss styles are defined here as package-level vars so that every
// view function references a single source of truth. Colour values are
// sourced exclusively from the PS 2026 brand spec; no value is guessed or
// approximated.
//
// PS palette quick-reference:
//   #C8DF00  PS Lime        — primary accent, active state, CTA
//   #7A8800  PS Olive       — secondary accent, feedback, hover
//   #454E00  PS Dark Olive  — tertiary, disabled / inactive accent
//   #3A3A3A  PS Dark Grey   — subtle text, dim labels, borders
//   #F0EDE4  PS Cream       — primary text on dark backgrounds

import "github.com/charmbracelet/lipgloss"

var (
	// psTitle is the app header: "punk.science · derpy".
	// Bold PS Lime so it reads as the primary landmark of the UI.
	psTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#C8DF00")).
		MarginBottom(1)

	// psTrack is the currently-playing track name (artist - title).
	// PS Cream: highest-contrast text, the thing the user most wants to read.
	psTrack = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F0EDE4")).
		MarginBottom(1)

	// psStatus is dim informational text: track count, play/pause state,
	// time display, indexer progress.
	psStatus = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3A3A3A")).
		MarginBottom(1)

	// psProgress colours the progress bar line (PS Lime).
	psProgress = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C8DF00"))

	// psControls is the bottom controls hint line.
	psControls = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3A3A3A")).
		MarginTop(2)

	// psPrompt is used for interactive overlay prompts (tag entry, Nostr key
	// entry). PS Lime draws the eye to the active input without alarming.
	psPrompt = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C8DF00")).
		MarginTop(1)

	// psNostr is the transient Nostr action-result line (earmark saved,
	// posted, queued, etc.). PS Olive is a step back from accent — visible
	// but not competing with the track name.
	psNostr = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7A8800")).
		MarginTop(1)

	// psIndexer wraps the spinner + fraction line shown while the background
	// sum-cache sweep is running.
	psIndexer = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3A3A3A")).
		MarginTop(1)

	// psTagHeader is the "tags" column header in the right-edge tag column.
	psTagHeader = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3A3A3A")).
		Bold(true)

	// psTag is each individual tag string in the right-edge column.
	psTag = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3A3A3A"))

	// psDotLime / psDotOlive / psDotDarkOlive are the three individual dot
	// styles for the indexer spinner. They are applied per-dot so the frame
	// rotation can reassign colours without rebuilding styles each tick.
	psDotLime = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C8DF00"))

	psDotOlive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7A8800"))

	psDotDarkOlive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#454E00"))
)
