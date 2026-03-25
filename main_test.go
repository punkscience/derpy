package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanMusicDirectory(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "dirplay-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create structure:
	// /Gospel/Jesus.mp3
	// /Gospel/Oldies.wav
	// /Pop/Song.flac
	// /Other.mp3

	structure := []string{
		"Gospel/Jesus.mp3",
		"Gospel/Oldies.wav",
		"Pop/Song.flac",
		"Other.mp3",
	}

	for _, p := range structure {
		fullPath := filepath.Join(tmpDir, p)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("fake content"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
	}

	tests := []struct {
		name     string
		keywords []string
		expected []string
	}{
		{
			name:     "No keywords",
			keywords: []string{},
			expected: []string{
				filepath.Join(tmpDir, "Gospel/Jesus.mp3"),
				filepath.Join(tmpDir, "Gospel/Oldies.wav"),
				filepath.Join(tmpDir, "Other.mp3"),
				filepath.Join(tmpDir, "Pop/Song.flac"),
			},
		},
		{
			name:     "Single keyword (jesus)",
			keywords: []string{"jesus"},
			expected: []string{
				filepath.Join(tmpDir, "Gospel/Jesus.mp3"),
			},
		},
		{
			name:     "Keyword in path (gospel)",
			keywords: []string{"gospel"},
			expected: []string{
				filepath.Join(tmpDir, "Gospel/Jesus.mp3"),
				filepath.Join(tmpDir, "Gospel/Oldies.wav"),
			},
		},
		{
			name:     "Multiple keywords (OR logic)",
			keywords: []string{"jesus", "pop"},
			expected: []string{
				filepath.Join(tmpDir, "Gospel/Jesus.mp3"),
				filepath.Join(tmpDir, "Pop/Song.flac"),
			},
		},
		{
			name:     "Case-insensitive matching",
			keywords: []string{"JESUS"},
			expected: []string{
				filepath.Join(tmpDir, "Gospel/Jesus.mp3"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := scanMusicDirectory(tmpDir, tt.keywords)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}
