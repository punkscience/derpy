package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "jazz", []string{"jazz"}},
		{"uppercase folds to lowercase", "JAZZ", []string{"jazz"}},
		{"mixed case folds", "JaZz", []string{"jazz"}},
		{"multiple", "jazz,piano,blues", []string{"jazz", "piano", "blues"}},
		{"trims internal whitespace", "  jazz ", []string{"jazz"}},
		{"dedupes case-insensitive", "jazz, JAZZ, Jazz", []string{"jazz"}},
		{"drops empty entries", "jazz,,piano", []string{"jazz", "piano"}},
		{"all empties → nil", " , , ,", nil},

		// The "Drum 'n' Bass" normalization rule from the grilling session:
		// letters + digits + spaces only; everything else stripped.
		{"drum n bass survives", "Drum n Bass", []string{"drum n bass"}},
		{"drum 'n' bass strips apostrophes to same canonical form",
			"Drum 'n' Bass", []string{"drum n bass"}},
		{"both forms dedupe together",
			"Drum n Bass, Drum 'n' Bass", []string{"drum n bass"}},

		// Digits preserved.
		{"808 bass keeps digits", "808 bass", []string{"808 bass"}},
		{"2step keeps digits", "2step", []string{"2step"}},
		{"90s keeps digits", "90s", []string{"90s"}},

		// Punctuation and other characters stripped.
		{"hyphen stripped", "trip-hop", []string{"triphop"}},
		{"slash stripped", "rock/pop", []string{"rockpop"}},
		{"accented letter stripped", "café", []string{"caf"}},
		{"emoji stripped", "jazz🎷", []string{"jazz"}},
		{"only punctuation → nil", "!@#$%", nil},

		// Internal whitespace collapses.
		{"multiple spaces collapse", "drum    n    bass", []string{"drum n bass"}},
		{"tabs treated as space", "drum\tn\tbass", []string{"drum n bass"}},

		// Preserves order of first occurrence.
		{"order preserved on dedup",
			"piano, jazz, piano, blues, jazz",
			[]string{"piano", "jazz", "blues"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTags(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NormalizeTags(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTagIndexGetSet(t *testing.T) {
	ti := NewTagIndex()

	// Empty: Get returns nil.
	if got := ti.Get("abc"); got != nil {
		t.Errorf("Get on empty index = %v, want nil", got)
	}

	// Set then Get.
	ti.Set("abc", []string{"jazz", "piano"})
	got := ti.Get("abc")
	if !reflect.DeepEqual(got, []string{"jazz", "piano"}) {
		t.Errorf("Get after Set = %v, want [jazz piano]", got)
	}

	// Get returns a defensive copy — mutating it must not affect the index.
	got[0] = "ZZZ"
	if again := ti.Get("abc"); again[0] != "jazz" {
		t.Errorf("Get did not return a defensive copy: index mutated to %v", again)
	}

	// Set with empty slice removes the entry.
	ti.Set("abc", nil)
	if got := ti.Get("abc"); got != nil {
		t.Errorf("Get after Set(nil) = %v, want nil (entry removed)", got)
	}
	if _, exists := ti.Tags["abc"]; exists {
		t.Errorf("entry remained in map after Set(nil); want removed")
	}
}

func TestTagIndexSumsWithAnyTag(t *testing.T) {
	ti := NewTagIndex()
	ti.Set("sumA", []string{"jazz", "piano"})
	ti.Set("sumB", []string{"dnb", "live"})
	ti.Set("sumC", []string{"piano", "live"})
	ti.Set("sumD", []string{"ambient"})

	tests := []struct {
		name  string
		query []string
		want  map[string]bool
	}{
		{"empty query → empty result", nil, map[string]bool{}},
		{"single tag, multiple matches",
			[]string{"piano"},
			map[string]bool{"sumA": true, "sumC": true}},
		{"OR across two tags",
			[]string{"jazz", "dnb"},
			map[string]bool{"sumA": true, "sumB": true}},
		{"no matches",
			[]string{"techno"},
			map[string]bool{}},
		{"unknown tag in query is harmless",
			[]string{"jazz", "techno"},
			map[string]bool{"sumA": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ti.SumsWithAnyTag(tt.query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SumsWithAnyTag(%v) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestTagIndexLoadSaveRoundTrip(t *testing.T) {
	// Redirect configDir to a temp dir so the test doesn't touch the real
	// ~/.config/derpy. We can't override configDir() directly without a
	// seam, so we lean on XDG_CONFIG_HOME which os.UserConfigDir honours
	// on Linux (and equivalent on other platforms via the same env hook).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// On Windows, os.UserConfigDir uses %AppData% rather than
	// XDG_CONFIG_HOME, so the test skips on Windows. The save/load logic
	// is platform-independent — covered by the in-memory tests above.
	if isWindows() {
		t.Skip("XDG_CONFIG_HOME not honoured by os.UserConfigDir on Windows")
	}

	orig := NewTagIndex()
	orig.Set("sumA", []string{"jazz", "piano"})
	orig.Set("sumB", []string{"dnb"})

	if err := SaveTagIndex(orig); err != nil {
		t.Fatalf("SaveTagIndex: %v", err)
	}

	loaded, err := LoadTagIndex()
	if err != nil {
		t.Fatalf("LoadTagIndex: %v", err)
	}
	if loaded.Version != tagIndexVersion {
		t.Errorf("loaded Version = %d, want %d", loaded.Version, tagIndexVersion)
	}
	if !reflect.DeepEqual(loaded.Get("sumA"), []string{"jazz", "piano"}) {
		t.Errorf("sumA round-trip mismatch: %v", loaded.Get("sumA"))
	}
	if !reflect.DeepEqual(loaded.Get("sumB"), []string{"dnb"}) {
		t.Errorf("sumB round-trip mismatch: %v", loaded.Get("sumB"))
	}
}

// --- Slice 5: --tags CLI pre-filter ---

func TestFilterPlaylistByTagsEmptyReturnsInput(t *testing.T) {
	paths := []string{"/a.flac", "/b.flac"}
	got := filterPlaylistByTags(paths, "", NewTagIndex(), NewSumCache())
	if !reflect.DeepEqual(got, paths) {
		t.Errorf("empty rawTags should return input unchanged; got %v", got)
	}
	// All-stripped input should likewise no-op (no tags after normalization).
	got2 := filterPlaylistByTags(paths, "!@#$", NewTagIndex(), NewSumCache())
	if !reflect.DeepEqual(got2, paths) {
		t.Errorf("all-stripped rawTags should return input unchanged; got %v", got2)
	}
}

func TestFilterPlaylistByTagsMatchesOnAnyTag(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.flac")
	b := filepath.Join(dir, "b.flac")
	c := filepath.Join(dir, "c.flac")
	for _, p := range []string{a, b, c} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ti := NewTagIndex()
	ti.Set("sumA", []string{"jazz", "piano"})
	ti.Set("sumB", []string{"dnb"})
	// sumC has no tags

	sc := NewSumCache()
	sc.Put(a, SumCacheEntry{Sum: "sumA"})
	sc.Put(b, SumCacheEntry{Sum: "sumB"})
	sc.Put(c, SumCacheEntry{Sum: "sumC"})

	// "jazz" → only a.flac
	got := filterPlaylistByTags([]string{a, b, c}, "jazz", ti, sc)
	if !reflect.DeepEqual(got, []string{a}) {
		t.Errorf("--tags jazz → %v, want [%s]", got, a)
	}

	// "jazz, dnb" (OR semantics) → a.flac and b.flac
	got = filterPlaylistByTags([]string{a, b, c}, "jazz, dnb", ti, sc)
	if !reflect.DeepEqual(got, []string{a, b}) {
		t.Errorf("--tags jazz,dnb → %v, want [%s %s]", got, a, b)
	}

	// "techno" → no matches
	got = filterPlaylistByTags([]string{a, b, c}, "techno", ti, sc)
	if got != nil {
		t.Errorf("--tags techno → %v, want nil", got)
	}
}

func TestFilterPlaylistByTagsDropsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.flac")
	missing := filepath.Join(dir, "ghost.flac")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// missing intentionally not written

	ti := NewTagIndex()
	ti.Set("sumA", []string{"jazz"})
	ti.Set("sumGhost", []string{"jazz"})

	sc := NewSumCache()
	sc.Put(a, SumCacheEntry{Sum: "sumA"})
	sc.Put(missing, SumCacheEntry{Sum: "sumGhost"})

	// Both sumA and sumGhost are tagged 'jazz', but only a.flac exists.
	got := filterPlaylistByTags([]string{a, missing}, "jazz", ti, sc)
	if !reflect.DeepEqual(got, []string{a}) {
		t.Errorf("stale cache entry should be dropped; got %v", got)
	}
}

func TestFilterPlaylistByTagsNormalizesQuery(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.flac")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ti := NewTagIndex()
	ti.Set("sumA", []string{"drum n bass"}) // stored canonical form

	sc := NewSumCache()
	sc.Put(a, SumCacheEntry{Sum: "sumA"})

	// User types the un-normalized form on the CLI — should still match.
	got := filterPlaylistByTags([]string{a}, "Drum 'n' Bass", ti, sc)
	if !reflect.DeepEqual(got, []string{a}) {
		t.Errorf("query 'Drum 'n' Bass' should normalize and match; got %v", got)
	}
}

func TestLoadTagIndexMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if isWindows() {
		t.Skip("XDG_CONFIG_HOME not honoured by os.UserConfigDir on Windows")
	}

	ti, err := LoadTagIndex()
	if err != nil {
		t.Fatalf("LoadTagIndex on missing file should not error: %v", err)
	}
	if ti.Version != tagIndexVersion {
		t.Errorf("missing-file load: Version = %d, want %d", ti.Version, tagIndexVersion)
	}
	if len(ti.Tags) != 0 {
		t.Errorf("missing-file load: Tags = %v, want empty", ti.Tags)
	}
}
