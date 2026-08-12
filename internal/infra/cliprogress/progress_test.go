package cliprogress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderBar(t *testing.T) {
	cases := []struct {
		name    string
		current int64
		total   int64
		width   int
		want    string
	}{
		{"zero percent", 0, 100, 10, "[----------]   0%"},
		{"fifty percent", 50, 100, 10, "[#####-----]  50%"},
		{"hundred percent", 100, 100, 10, "[##########] 100%"},
		{"unknown total negative", 5, -1, 10, "downloading... (size unknown)"},
		{"unknown total zero", 5, 0, 10, "downloading... (size unknown)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RenderBar(c.current, c.total, c.width)
			if got != c.want {
				t.Fatalf("RenderBar(%d, %d, %d) = %q, want %q", c.current, c.total, c.width, got, c.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{210, "210 B"},
		{2_000, "2.0 KB"},
		{3_600_000, "3.6 MB"},
		{1_200_000_000, "1.2 GB"},
	}
	for _, c := range cases {
		got := FormatBytes(c.n)
		if got != c.want {
			t.Fatalf("FormatBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestBarInteractiveThrottlesRedrawsAndAlwaysFlushesCompletion(t *testing.T) {
	var buf bytes.Buffer
	bar := NewBar(&buf, true)
	now := time.Unix(0, 0)
	bar.Now = func() time.Time { return now }

	bar.Update(10, 100, "Download")
	if got := strings.Count(buf.String(), "\r"); got != 1 {
		t.Fatalf("redraw count after first update = %d, want 1", got)
	}

	now = now.Add(10 * time.Millisecond)
	bar.Update(20, 100, "Download")
	if got := strings.Count(buf.String(), "\r"); got != 1 {
		t.Fatalf("redraw count after throttled update = %d, want 1 (should be skipped)", got)
	}

	now = now.Add(200 * time.Millisecond)
	bar.Update(30, 100, "Download")
	if got := strings.Count(buf.String(), "\r"); got != 2 {
		t.Fatalf("redraw count after elapsed update = %d, want 2", got)
	}

	// Completion must always flush even if called immediately after the previous redraw.
	bar.Update(100, 100, "Download")
	if got := strings.Count(buf.String(), "\r"); got != 3 {
		t.Fatalf("redraw count after completion = %d, want 3 (completion always flushes)", got)
	}
	if !strings.Contains(buf.String(), "100%") {
		t.Fatalf("output = %q, want final 100%% frame", buf.String())
	}
}

func TestBarNonInteractiveEmitsBoundedMilestoneLines(t *testing.T) {
	var buf bytes.Buffer
	bar := NewBar(&buf, false)

	bar.Update(10, 100, "Download")  // 10%: below first milestone, no line
	bar.Update(30, 100, "Download")  // 30%: crosses 25% milestone
	bar.Update(60, 100, "Download")  // 60%: crosses 50% milestone
	bar.Update(100, 100, "Download") // 100%: mandatory final line

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %#v, want exactly 3 milestone lines", lines)
	}
	for _, want := range []string{"30%", "60%", "100%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output = %q, want to contain %q", out, want)
		}
	}
	if strings.Contains(out, "\r") || strings.Contains(out, "\x1b[") {
		t.Fatalf("non-interactive output = %q, want no cursor control sequences", out)
	}
}

func TestBarNonInteractiveUnknownTotalPrintsOnce(t *testing.T) {
	var buf bytes.Buffer
	bar := NewBar(&buf, false)

	bar.Update(500, -1, "Download")
	bar.Update(1500, -1, "Download")
	bar.Update(3000, -1, "Download")

	out := buf.String()
	if got := strings.Count(out, "size unknown"); got != 1 {
		t.Fatalf("size-unknown line count = %d, want exactly 1, output = %q", got, out)
	}
}

func TestBarNilSafe(t *testing.T) {
	var bar *Bar
	bar.Update(1, 2, "Download") // must not panic
}

func TestChecklistInteractiveLifecycle(t *testing.T) {
	var buf bytes.Buffer
	steps := []Step{
		{Key: "resolve", Label: "Resolve"},
		{Key: "download", Label: "Download"},
		{Key: "verify", Label: "Verify"},
		{Key: "stop", Label: "Stop"},
	}
	cl := NewChecklist(&buf, true, steps)

	cl.Activate("resolve", "Resolving target release")
	cl.Activate("download", "Downloading regixtry_1.2.3_linux_amd64.tar.gz")
	cl.Activate("verify", "Verifying regixtry_1.2.3_linux_amd64.tar.gz")

	out := buf.String()
	if !strings.Contains(out, "\x1b[4A") {
		t.Fatalf("output = %q, want cursor-up escape for the 4-step block", out)
	}
	if !strings.Contains(out, "\x1b[K") {
		t.Fatalf("output = %q, want clear-to-end-of-line escapes", out)
	}
	if !strings.Contains(out, "✓ Resolve") {
		t.Fatalf("output = %q, want completed marker for resolve", out)
	}
	if !strings.Contains(out, "✓ Download") {
		t.Fatalf("output = %q, want completed marker for download", out)
	}
	if !strings.Contains(out, "▸ Verify: Verifying regixtry_1.2.3_linux_amd64.tar.gz") {
		t.Fatalf("output = %q, want active marker with detail for verify", out)
	}
	if !strings.Contains(out, "○ Stop") {
		t.Fatalf("output = %q, want pending marker for stop", out)
	}
}

func TestChecklistInteractiveFailPath(t *testing.T) {
	var buf bytes.Buffer
	steps := []Step{
		{Key: "resolve", Label: "Resolve"},
		{Key: "download", Label: "Download"},
	}
	cl := NewChecklist(&buf, true, steps)

	cl.Activate("resolve", "")
	cl.Activate("download", "Downloading x.tar.gz")
	cl.Fail("download")

	out := buf.String()
	if !strings.Contains(out, "✗ Download: Downloading x.tar.gz") {
		t.Fatalf("output = %q, want failed marker with detail for download", out)
	}
}

func TestChecklistNonInteractiveLifecycle(t *testing.T) {
	var buf bytes.Buffer
	steps := []Step{
		{Key: "resolve", Label: "Resolve"},
		{Key: "download", Label: "Download"},
	}
	cl := NewChecklist(&buf, false, steps)

	cl.Activate("resolve", "Resolving target release")
	cl.Activate("download", "Downloading x.tar.gz")
	cl.Complete("download")

	out := buf.String()
	for _, want := range []string{
		"[1/2] Resolve: Resolving target release",
		"[2/2] Download: Downloading x.tar.gz",
		"[2/2] Download: done",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output = %q, want line containing %q", out, want)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("non-interactive output = %q, want no ANSI escape sequences", out)
	}
}

func TestChecklistNonInteractiveFailPath(t *testing.T) {
	var buf bytes.Buffer
	steps := []Step{{Key: "verify", Label: "Verify"}}
	cl := NewChecklist(&buf, false, steps)

	cl.Activate("verify", "Verifying")
	cl.Fail("verify")

	if !strings.Contains(buf.String(), "[1/1] Verify: failed") {
		t.Fatalf("output = %q, want failed line", buf.String())
	}
}

func TestChecklistIgnoresUnknownKey(t *testing.T) {
	var buf bytes.Buffer
	cl := NewChecklist(&buf, false, []Step{{Key: "resolve", Label: "Resolve"}})

	cl.Activate("does-not-exist", "detail")
	cl.Complete("does-not-exist")
	cl.Fail("does-not-exist")

	if buf.Len() != 0 {
		t.Fatalf("output = %q, want no output for unknown key", buf.String())
	}
}

func TestChecklistNilSafe(t *testing.T) {
	var cl *Checklist
	cl.Activate("resolve", "detail") // must not panic
	cl.Complete("resolve")
	cl.Fail("resolve")
}
