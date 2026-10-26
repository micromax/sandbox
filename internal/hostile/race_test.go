//go:build race

package hostile_test

// raceEnabled scales time bounds: the race detector slows the host side a lot.
const raceEnabled = true
