// Package cliprogress renders CLI progress feedback: a byte-level progress
// bar for a single download-like operation, and a step checklist for
// discrete lifecycle stages. Both pieces are stdlib-only and carry no
// dependency on any domain/app/ports type so they can be reused by any CLI
// command that reports step-based or byte-based progress.
package cliprogress

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	defaultBarWidth = 24
	// barMinRedrawInterval throttles interactive redraws so a fast local
	// transfer does not flood the terminal with one frame per Read() call.
	barMinRedrawInterval = 90 * time.Millisecond
)

// RenderBar renders a fixed-width ASCII progress bar with a trailing
// percentage, e.g. "[#####-----]  50%". ASCII '#'/'-' is used instead of
// Unicode block characters to match this codebase's existing non-Bubbletea
// CLI rendering convention (see upgradeProgressWriter's prior bar).
//
// A non-positive total means the size is unknown; RenderBar then returns a
// distinct indeterminate message instead of dividing by zero.
func RenderBar(current int64, total int64, width int) string {
	if total <= 0 {
		return "downloading... (size unknown)"
	}
	if width <= 0 {
		width = defaultBarWidth
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	percent := int(current * 100 / total)
	filled := width * percent / 100
	if filled > width {
		filled = width
	}
	return fmt.Sprintf("[%s%s] %3d%%", strings.Repeat("#", filled), strings.Repeat("-", width-filled), percent)
}

// FormatBytes renders a byte count using decimal (1000-based) units, e.g.
// "3.6 MB". Decimal units are chosen over binary KiB/MiB because download
// sizes are conventionally advertised in decimal units by release tooling.
func FormatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1000.0
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.1f KB", float64(n)/unit)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/(unit*unit))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(unit*unit*unit))
	}
}

// Bar is a small stateful wrapper that redraws a byte-level progress line in
// place on an interactive terminal, or emits a bounded set of milestone
// lines when the output is not interactive (e.g. piped to a file or CI log).
type Bar struct {
	out         io.Writer
	interactive bool
	width       int

	// Now is injectable for deterministic tests; it defaults to time.Now.
	Now func() time.Time

	lastUpdate time.Time
	lastWidth  int
	started    bool

	lastMilestone     int
	printedUnknownOne bool
}

// NewBar constructs a Bar writing to out. interactive mirrors the
// convention already used by upgradeProgressWriter: when the destination is
// not a TTY, Bar prints one bounded line per milestone instead of redrawing
// in place.
func NewBar(out io.Writer, interactive bool) *Bar {
	return &Bar{
		out:         out,
		interactive: interactive,
		width:       defaultBarWidth,
	}
}

func (b *Bar) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Update reports current/total bytes transferred for label. It is safe to
// call on a nil *Bar.
func (b *Bar) Update(current int64, total int64, label string) {
	if b == nil || b.out == nil {
		return
	}
	if b.interactive {
		b.updateInteractive(current, total, label)
		return
	}
	b.updateNonInteractive(current, total, label)
}

func (b *Bar) updateInteractive(current int64, total int64, label string) {
	done := total > 0 && current >= total
	now := b.now()
	if b.started && !done && now.Sub(b.lastUpdate) < barMinRedrawInterval {
		return
	}
	b.lastUpdate = now
	b.started = true

	line := formatBarLine(current, total, b.width, label)
	// Reuses the carriage-return + pad-with-spaces-to-clear-previous-line
	// technique already established by upgradeProgressWriter.Advance.
	padding := ""
	if delta := b.lastWidth - len(line); delta > 0 {
		padding = strings.Repeat(" ", delta)
	}
	fmt.Fprintf(b.out, "\r%s%s", line, padding)
	b.lastWidth = len(line)
}

func (b *Bar) updateNonInteractive(current int64, total int64, label string) {
	if total <= 0 {
		if !b.printedUnknownOne {
			b.printedUnknownOne = true
			fmt.Fprintf(b.out, "%s: %s\n", label, RenderBar(current, total, b.width))
		}
		return
	}
	percent := int(current * 100 / total)
	if percent > 100 {
		percent = 100
	}
	milestone := percent - (percent % 25)
	done := current >= total
	if done {
		milestone = 100
	}
	if milestone <= b.lastMilestone {
		return
	}
	b.lastMilestone = milestone
	fmt.Fprintf(b.out, "%s: %d%% (%s / %s)\n", label, percent, FormatBytes(current), FormatBytes(total))
}

func formatBarLine(current int64, total int64, width int, label string) string {
	bar := RenderBar(current, total, width)
	if total <= 0 {
		return fmt.Sprintf("%s: %s", label, bar)
	}
	return fmt.Sprintf("%s %s (%s / %s)", bar, label, FormatBytes(current), FormatBytes(total))
}

// Step describes one discrete lifecycle stage tracked by a Checklist.
type Step struct {
	Key   string
	Label string
}

type stepStatus int

const (
	stepPending stepStatus = iota
	stepActive
	stepDone
	stepFailed
)

// Checklist renders a set of discrete lifecycle steps as a polished
// checklist on an interactive terminal (checkmark for done, an active
// marker for the in-progress step, dim markers for pending steps), or as one
// plain line per event when the output is not interactive.
type Checklist struct {
	out         io.Writer
	interactive bool
	steps       []Step
	index       map[string]int
	status      []stepStatus
	detail      []string
	printed     bool
}

// NewChecklist constructs a Checklist for the given ordered steps.
func NewChecklist(out io.Writer, interactive bool, steps []Step) *Checklist {
	index := make(map[string]int, len(steps))
	for i, step := range steps {
		index[step.Key] = i
	}
	return &Checklist{
		out:         out,
		interactive: interactive,
		steps:       steps,
		index:       index,
		status:      make([]stepStatus, len(steps)),
		detail:      make([]string, len(steps)),
	}
}

// Activate marks key as the current in-progress step with detail, and marks
// every earlier step as done. Unknown keys are ignored (no panic), and it is
// safe to call on a nil *Checklist.
func (c *Checklist) Activate(key string, detail string) {
	if c == nil || c.out == nil {
		return
	}
	i, ok := c.index[key]
	if !ok {
		return
	}
	for j := 0; j < i; j++ {
		if c.status[j] != stepFailed {
			c.status[j] = stepDone
		}
	}
	c.status[i] = stepActive
	c.detail[i] = detail
	c.render(i)
}

// Complete marks key as done. Unknown keys are ignored, and it is safe to
// call on a nil *Checklist.
func (c *Checklist) Complete(key string) {
	if c == nil || c.out == nil {
		return
	}
	i, ok := c.index[key]
	if !ok {
		return
	}
	c.status[i] = stepDone
	c.render(i)
}

// Fail marks key as failed. Unknown keys are ignored, and it is safe to call
// on a nil *Checklist.
func (c *Checklist) Fail(key string) {
	if c == nil || c.out == nil {
		return
	}
	i, ok := c.index[key]
	if !ok {
		return
	}
	c.status[i] = stepFailed
	c.render(i)
}

func (c *Checklist) render(changed int) {
	if c.interactive {
		c.renderInteractive()
		return
	}
	c.renderNonInteractiveLine(changed)
}

func (c *Checklist) renderInteractive() {
	if c.printed {
		// Cursor-up by the full block height, then rewrite every line ending
		// in an explicit clear-to-end-of-line sequence: more robust than
		// width-padding because it needs no knowledge of the previous frame.
		fmt.Fprintf(c.out, "\x1b[%dA", len(c.steps))
	}
	for i, step := range c.steps {
		fmt.Fprintf(c.out, "\r%s\x1b[K\n", c.formatInteractiveLine(i, step))
	}
	c.printed = true
}

func (c *Checklist) formatInteractiveLine(i int, step Step) string {
	marker := "○"
	suffix := ""
	switch c.status[i] {
	case stepDone:
		marker = "✓"
	case stepActive:
		marker = "▸"
		if d := strings.TrimSpace(c.detail[i]); d != "" {
			suffix = ": " + d
		}
	case stepFailed:
		marker = "✗"
		if d := strings.TrimSpace(c.detail[i]); d != "" {
			suffix = ": " + d
		}
	}
	return fmt.Sprintf("  %s %s%s", marker, step.Label, suffix)
}

func (c *Checklist) renderNonInteractiveLine(i int) {
	label := c.steps[i].Label
	total := len(c.steps)
	switch c.status[i] {
	case stepDone:
		fmt.Fprintf(c.out, "[%d/%d] %s: done\n", i+1, total, label)
	case stepFailed:
		fmt.Fprintf(c.out, "[%d/%d] %s: failed\n", i+1, total, label)
	default:
		detail := strings.TrimSpace(c.detail[i])
		if detail == "" {
			fmt.Fprintf(c.out, "[%d/%d] %s\n", i+1, total, label)
			return
		}
		fmt.Fprintf(c.out, "[%d/%d] %s: %s\n", i+1, total, label, detail)
	}
}
