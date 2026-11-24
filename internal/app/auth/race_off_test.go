//go:build !race

package auth

// raceEnabled reports whether the race detector is on (it slows code 5-10x).
const raceEnabled = false
