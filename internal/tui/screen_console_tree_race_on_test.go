//go:build race

package tui

// treeFrameBudgetFactor relaxes the frame-time budget under the race detector,
// which slows everything down several times.
const treeFrameBudgetFactor = 8
