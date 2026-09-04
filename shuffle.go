package main

import (
	"math/rand"
	"time"
)

// shufflePlaylist shuffles the playlist using Fisher-Yates algorithm
func shufflePlaylist(playlist []string) {
	// Create a new random source
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	
	// Fisher-Yates shuffle
	for i := len(playlist) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		playlist[i], playlist[j] = playlist[j], playlist[i]
	}
}