package main

import (
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"

	core "github.com/punkscience/earmark/earmark-core"
)

// testPickerChannels returns two channels so the picker has a personal row
// plus two channel rows to move between and toggle.
func testPickerChannels() []core.Channel {
	return []core.Channel{
		{Descriptor: core.ChannelDescriptor{ID: "chan-lisette", Name: "Lisette"}},
		{Descriptor: core.ChannelDescriptor{ID: "chan-alex", Name: "Alex"}},
	}
}

// testPickerModel returns a playing model with channels loaded and a Nostr
// key configured, i.e. the state where [E] opens the target picker.
func testPickerModel(t *testing.T) *PlayerModel {
	t.Helper()
	t.Setenv("DERPY_NOSTR_KEY", nostr.GeneratePrivateKey())
	m := NewPlayerModel([]string{"a.flac"})
	m.playing = true
	m.channels = testPickerChannels()
	return m
}

// TestEarmarkOpensPickerWithPersonalSelected verifies [E] opens the target
// picker with personal pre-selected and no channels ticked, so the
// pre-channels flow is still [E] then enter.
func TestEarmarkOpensPickerWithPersonalSelected(t *testing.T) {
	m := testPickerModel(t)
	m.Update(keyMsg("e"))
	if !m.channelPicker {
		t.Fatalf("channelPicker = false after [E] with channels available")
	}
	if !m.channelPersonal {
		t.Errorf("channelPersonal = false on open; personal should start selected")
	}
	if m.channelPickerSelected() != 1 {
		t.Errorf("selected = %d, want 1 (personal only)", m.channelPickerSelected())
	}
}

// TestPickerSpaceTogglesPersonal verifies the personal row is a toggle: a
// user who only wants a channel share can untick personal.
func TestPickerSpaceTogglesPersonal(t *testing.T) {
	m := testPickerModel(t)
	m.Update(keyMsg("e"))
	if m.channelCursor != 0 {
		t.Fatalf("setup: channelCursor = %d, want 0 (personal row)", m.channelCursor)
	}
	m.Update(keyMsg(" "))
	if m.channelPersonal {
		t.Errorf("channelPersonal still true after space on the personal row")
	}
	m.Update(keyMsg(" "))
	if !m.channelPersonal {
		t.Errorf("channelPersonal false after toggling twice; space should flip it back")
	}
}

// TestPickerSpaceTogglesChannel verifies space on a channel row ticks and
// unticks that channel without touching personal.
func TestPickerSpaceTogglesChannel(t *testing.T) {
	m := testPickerModel(t)
	m.Update(keyMsg("e"))
	m.Update(keyMsg("down")) // cursor 1 = Lisette
	m.Update(keyMsg(" "))
	if !m.channelTargets["chan-lisette"] {
		t.Fatalf("Lisette not selected after space")
	}
	if !m.channelPersonal {
		t.Errorf("toggling a channel changed personal; rows are independent")
	}
	m.Update(keyMsg(" "))
	if m.channelTargets["chan-lisette"] {
		t.Errorf("Lisette still selected after second space")
	}
}

// TestPickerEnterWithNothingSelectedStaysOpen verifies enter with every box
// unticked keeps the picker up instead of sending the earmark nowhere.
func TestPickerEnterWithNothingSelectedStaysOpen(t *testing.T) {
	m := testPickerModel(t)
	m.Update(keyMsg("e"))
	m.Update(keyMsg(" ")) // untick personal; channels untouched
	_, cmd := m.Update(keyMsg("enter"))
	if !m.channelPicker {
		t.Errorf("picker closed on enter with zero targets; must stay open")
	}
	if cmd != nil {
		t.Errorf("expected no command when nothing is selected, got one")
	}
}

// TestPickerEnterChannelOnlyConfirms verifies a share to just "Lisette"
// (personal unticked) closes the picker and reports a channel share —
// personal is not forced.
func TestPickerEnterChannelOnlyConfirms(t *testing.T) {
	m := testPickerModel(t)
	m.Update(keyMsg("e"))
	m.Update(keyMsg(" "))    // untick personal
	m.Update(keyMsg("down")) // cursor 1 = Lisette
	m.Update(keyMsg(" "))    // tick Lisette
	_, cmd := m.Update(keyMsg("enter"))
	if m.channelPicker {
		t.Errorf("picker still open after enter with Lisette selected")
	}
	if got := m.nostrStatus; got != "Nostr: sharing to 1 channel(s)..." {
		t.Errorf("nostrStatus = %q, want channel-only share message", got)
	}
	if cmd == nil {
		t.Errorf("expected a save command for the channel-only share, got nil")
	}
}

// TestPickerEnterPersonalOnlyKeepsOldFlow verifies [E] then enter with the
// defaults keeps the track personally and nothing else.
func TestPickerEnterPersonalOnlyKeepsOldFlow(t *testing.T) {
	m := testPickerModel(t)
	m.Update(keyMsg("e"))
	_, cmd := m.Update(keyMsg("enter"))
	if m.channelPicker {
		t.Errorf("picker still open after enter")
	}
	if got := m.nostrStatus; got != "Nostr: saving earmark..." {
		t.Errorf("nostrStatus = %q, want %q", got, "Nostr: saving earmark...")
	}
	if cmd == nil {
		t.Errorf("expected a save command for the personal keep, got nil")
	}
}

// TestPickerEnterPersonalPlusChannel verifies ticking a channel on top of
// personal reports the combined keep-and-share.
func TestPickerEnterPersonalPlusChannel(t *testing.T) {
	m := testPickerModel(t)
	m.Update(keyMsg("e"))
	m.Update(keyMsg("down")) // cursor 1 = Lisette
	m.Update(keyMsg(" "))    // tick Lisette; personal still ticked
	_, cmd := m.Update(keyMsg("enter"))
	if m.channelPicker {
		t.Errorf("picker still open after enter")
	}
	if got := m.nostrStatus; got != "Nostr: earmarking and sharing to 1 channel(s)..." {
		t.Errorf("nostrStatus = %q, want keep-and-share message", got)
	}
	if cmd == nil {
		t.Errorf("expected a save command, got nil")
	}
}

// TestChannelPickerSelectedCounts verifies the helper counts personal plus
// ticked channels, ignoring unticked (false-valued) map entries left behind
// by toggling a channel off again.
func TestChannelPickerSelectedCounts(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.channels = testPickerChannels()
	if got := m.channelPickerSelected(); got != 0 {
		t.Errorf("selected = %d with nothing ticked, want 0", got)
	}
	m.channelPersonal = true
	m.channelTargets = map[string]bool{"chan-lisette": true, "chan-alex": false}
	if got := m.channelPickerSelected(); got != 2 {
		t.Errorf("selected = %d, want 2 (personal + Lisette)", got)
	}
}

// TestPickerViewShowsPersonalCheckbox verifies the personal row renders as a
// checkbox reflecting its state, and the hint demands a target when nothing
// is checked.
func TestPickerViewShowsPersonalCheckbox(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.channels = testPickerChannels()
	m.channelPicker = true
	m.channelCursor = 1 // highlight Lisette so the personal row is unstyled
	m.channelPersonal = true
	m.channelTargets = map[string]bool{}
	if got := m.View(); !strings.Contains(got, "[x] personal") {
		t.Errorf("view missing checked personal row:\n%s", got)
	}

	m.channelPersonal = false
	got := m.View()
	if !strings.Contains(got, "[ ] personal") {
		t.Errorf("view missing unchecked personal row:\n%s", got)
	}
	if !strings.Contains(got, "select at least one target") {
		t.Errorf("view missing at-least-one hint with zero targets:\n%s", got)
	}
}

// TestEarmarkWithoutChannelsSkipsPicker verifies the single-destination case:
// with no channels there is no choice to make, so [E] keeps personally
// without opening the picker.
func TestEarmarkWithoutChannelsSkipsPicker(t *testing.T) {
	t.Setenv("DERPY_NOSTR_KEY", nostr.GeneratePrivateKey())
	m := NewPlayerModel([]string{"a.flac"})
	m.playing = true
	// channels stays empty.
	_, cmd := m.Update(keyMsg("e"))
	if m.channelPicker {
		t.Errorf("picker opened with no channels; earmark should go straight to personal")
	}
	if got := m.nostrStatus; got != "Nostr: saving earmark..." {
		t.Errorf("nostrStatus = %q, want %q", got, "Nostr: saving earmark...")
	}
	if cmd == nil {
		t.Errorf("expected a save command, got nil")
	}
}

// TestChannelOnlyShareStatus verifies the result of a channel-only share
// reads as a share, not as a personal earmark that never happened.
func TestChannelOnlyShareStatus(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(nostrPublishedMsg{action: "earmark", shared: true})
	if got := m.nostrStatus; got != "Nostr: shared to channel(s)!" {
		t.Errorf("nostrStatus = %q, want channel-only success message", got)
	}
	m.Update(nostrPublishedMsg{action: "earmark"})
	if got := m.nostrStatus; got != "Nostr: earmark saved!" {
		t.Errorf("nostrStatus = %q, want personal success message", got)
	}
}
