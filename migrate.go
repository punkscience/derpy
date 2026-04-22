package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// MigrationResult describes what MigrateLegacyEarmarks did, for callers that
// want to surface a one-line message to the user.
type MigrationResult struct {
	// Migrated is the number of earmarks copied from the legacy list to the
	// new list. Zero means nothing needed to move.
	Migrated int
	// AlreadyDone is true when the persisted config flag short-circuited the
	// migration without any relay traffic.
	AlreadyDone bool
	// Skipped is true when the new list was already populated, so the legacy
	// list was intentionally left untouched.
	Skipped bool
}

// MigrateLegacyEarmarks performs the one-shot migration of earmarks from the
// "dirplay-earmarks" d-tag to "derpy-earmarks" after the project rename.
//
// Behaviour:
//   - If the persisted LegacyEarmarksMigrated flag is true, returns immediately
//     with zero relay traffic. This is the steady-state path.
//   - Otherwise, fetches the new list. If non-empty, the user already has
//     fresh earmarks under the new tag and we leave the legacy list strictly
//     alone (the safe assumption is that they intentionally moved on); the
//     migration flag is then persisted so we never check again.
//   - Otherwise, fetches the legacy list. If empty, marks the flag and exits.
//   - Otherwise, republishes every legacy entry under the new d-tag, then
//     publishes a NIP-09 (kind 5) deletion event referencing the legacy
//     addressable coordinate so relays can drop the old event. Finally,
//     persists the flag so we never repeat the work.
//
// All relay round-trips are guarded by short timeouts. The function never
// returns an error for partial failures it can recover from on a future run —
// the migration flag is only set on a clean success path.
func MigrateLegacyEarmarks(hexPrivKey string) (MigrationResult, error) {
	cfg, err := LoadConfig()
	if err != nil {
		// Treat config-load failure as "do not run"; the user has bigger
		// problems and we don't want to thrash relays trying to recover.
		return MigrationResult{}, fmt.Errorf("could not load config: %w", err)
	}
	if cfg.LegacyEarmarksMigrated {
		return MigrationResult{AlreadyDone: true}, nil
	}

	// Step 1: check the new list. If the user already has earmarks under
	// "derpy-earmarks" we are done — never touch the legacy list.
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 15*time.Second)
	current, err := fetchEarmarksCtx(checkCtx, hexPrivKey, earmarkListTag)
	checkCancel()
	if err != nil {
		return MigrationResult{}, fmt.Errorf("could not check current earmark list: %w", err)
	}
	if len(current) > 0 {
		cfg.LegacyEarmarksMigrated = true
		_ = SaveConfig(cfg)
		return MigrationResult{Skipped: true}, nil
	}

	// Step 2: fetch the legacy list.
	legacyCtx, legacyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	legacy, err := fetchEarmarksCtx(legacyCtx, hexPrivKey, legacyEarmarkListTag)
	legacyCancel()
	if err != nil {
		// A fetch error here is non-fatal: the legacy list may genuinely
		// not exist (decryption failure on an empty result is impossible
		// since queryRelays returns nil; any error here is a real network
		// or decode failure). Don't persist the flag — try again next time.
		return MigrationResult{}, fmt.Errorf("could not fetch legacy earmark list: %w", err)
	}
	if len(legacy) == 0 {
		// Nothing to migrate — and nothing to delete on the relays either.
		cfg.LegacyEarmarksMigrated = true
		_ = SaveConfig(cfg)
		return MigrationResult{}, nil
	}

	// Step 3: republish under the new d-tag.
	publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := publishEarmarks(publishCtx, hexPrivKey, legacy); err != nil {
		publishCancel()
		return MigrationResult{}, fmt.Errorf("could not republish earmarks under new tag: %w", err)
	}
	publishCancel()

	// Step 4: ask relays to drop the legacy addressable event (NIP-09).
	// Best-effort: a delete failure does not roll back the migration — the
	// data is safely under the new tag, and the legacy list will simply
	// linger on relays until it eventually ages out.
	deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 15*time.Second)
	_ = publishLegacyDeletion(deleteCtx, hexPrivKey)
	deleteCancel()

	// Step 5: persist the flag so we never run again.
	cfg.LegacyEarmarksMigrated = true
	if err := SaveConfig(cfg); err != nil {
		// The migration itself succeeded; the flag write failed. Surface a
		// warning but report the migration as done — worst case we re-run
		// the cheap "new list non-empty" branch on the next invocation.
		return MigrationResult{Migrated: len(legacy)},
			fmt.Errorf("migration succeeded but could not persist flag: %w", err)
	}

	return MigrationResult{Migrated: len(legacy)}, nil
}

// publishLegacyDeletion publishes a NIP-09 kind-5 deletion event referencing
// the legacy "dirplay-earmarks" addressable coordinate. Relays that honour
// NIP-09 will drop the legacy kind-30001 event for this (pubkey, d-tag).
//
// We use an "a" tag (not "e") because the target is an addressable event:
//
//	a = "<kind>:<pubkey>:<d-identifier>"
//
// per NIP-01 §addressable events and NIP-09 §deleting addressable events.
func publishLegacyDeletion(ctx context.Context, hexPrivKey string) error {
	pubHex, err := nostr.GetPublicKey(hexPrivKey)
	if err != nil {
		return fmt.Errorf("could not derive public key: %w", err)
	}

	coord := fmt.Sprintf("%d:%s:%s", earmarkKind, pubHex, legacyEarmarkListTag)

	ev := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      5, // NIP-09 deletion
		Content:   "derpy: superseding legacy dirplay-earmarks list after project rename",
		Tags: nostr.Tags{
			{"a", coord},
			// k tag clarifies which kind we're deleting; some relays use it
			// as a hint when applying the deletion.
			{"k", fmt.Sprintf("%d", earmarkKind)},
		},
	}
	if err := ev.Sign(hexPrivKey); err != nil {
		return fmt.Errorf("could not sign deletion event: %w", err)
	}

	return publishToRelays(ctx, LoadNostrRelays(), ev)
}
