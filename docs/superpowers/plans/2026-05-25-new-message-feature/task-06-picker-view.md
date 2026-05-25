# Task 6: Picker view rendering

**Goal:** Add `View` and `ViewOverlay` so the picker can render. Title bar, pill bar + query input, scrollable result list with `[ext]` tag for externals, footer with key hints and `N / 8` counter. Composited via `overlay.DimmedOverlay` like `channelfinder`.

**Files:**
- Create: `internal/ui/newmessagepicker/view.go`

---

> **Why no automated tests?** Rendering output is ANSI-styled and width-dependent. The codebase has no golden-file infra; visual correctness is verified manually (Task 14). The structural correctness of the view (which rows appear, in what order) is already covered by Tasks 3–5 via `m.filtered`.

- [ ] **Step 1: Create the view file**

Write `internal/ui/newmessagepicker/view.go`:

```go
package newmessagepicker

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// View renders just the modal box. Use ViewOverlay for the
// dimmed-backdrop composite that the App actually paints.
func (m Model) View(termWidth int) string {
	return m.renderBox(termWidth)
}

// ViewOverlay renders the modal centered on a dimmed copy of the
// current screen. background is the already-rendered base screen.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}

func (m Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := termWidth / 2
	if overlayWidth < 40 {
		overlayWidth = 40
	}
	if overlayWidth > 80 {
		overlayWidth = 80
	}
	innerWidth := overlayWidth - 4
	bg := styles.Background

	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render("New message")

	input := m.renderInputRow(innerWidth, bg)
	rows := m.renderResultRows(innerWidth, bg)
	footer := m.renderFooter(innerWidth, bg)

	content := title + "\n" + input + "\n\n" + strings.Join(rows, "\n") + "\n\n" + footer

	// Re-paint modal bg+fg after every ANSI reset emitted by inner
	// styled spans, otherwise trailing spaces inherit the dimmed-
	// app background. Same fix channelfinder uses.
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}

// renderInputRow renders the "To: [pill] [pill] |query█" row.
func (m Model) renderInputRow(innerWidth int, bg lipgloss.Color) string {
	prefix := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("To: ")

	var b strings.Builder
	b.WriteString(prefix)

	pillStyle := lipgloss.NewStyle().
		Background(styles.Primary).
		Foreground(styles.Background).
		Padding(0, 1)

	for _, u := range m.users {
		if _, ok := m.selected[u.ID]; !ok {
			continue
		}
		b.WriteString(pillStyle.Render(u.DisplayName))
		b.WriteString(" ")
	}

	queryStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
	if m.query == "" && len(m.selected) == 0 {
		placeholder := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("type a name…")
		b.WriteString(placeholder)
	} else {
		b.WriteString(queryStyle.Render(m.query))
		b.WriteString(queryStyle.Render("█"))
	}

	row := b.String()
	if lipgloss.Width(row) > innerWidth {
		row = truncate.StringWithTail(row, uint(innerWidth), "…")
	}
	return row
}

// renderResultRows produces up to 10 visible result rows with a
// scrollbar when the list overflows. Highlighted row gets a left bar
// and the Primary foreground.
func (m Model) renderResultRows(innerWidth int, bg lipgloss.Color) []string {
	const maxVisible = 10
	total := len(m.filtered)

	if total == 0 {
		var msg string
		if m.query != "" {
			msg = fmt.Sprintf("No users match %q", m.query)
		} else {
			msg = "No users available"
		}
		return []string{
			lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.TextMuted).
				Italic(true).
				Render(msg),
		}
	}

	visible := maxVisible
	if visible > total {
		visible = total
	}

	startIdx := 0
	if m.highlight >= visible {
		startIdx = m.highlight - visible + 1
	}
	endIdx := startIdx + visible
	if endIdx > total {
		endIdx = total
		startIdx = endIdx - visible
		if startIdx < 0 {
			startIdx = 0
		}
	}

	showScrollbar := total > visible
	contentWidth := innerWidth - 1 // 1 col for the highlight indicator
	if showScrollbar {
		contentWidth--
	}

	var thumbStart, thumbEnd int
	if showScrollbar {
		thumbHeight := visible * visible / total
		if thumbHeight < 1 {
			thumbHeight = 1
		}
		denom := total - visible
		if denom < 1 {
			denom = 1
		}
		thumbStart = startIdx * (visible - thumbHeight) / denom
		if thumbStart < 0 {
			thumbStart = 0
		}
		if thumbStart > visible-thumbHeight {
			thumbStart = visible - thumbHeight
		}
		thumbEnd = thumbStart + thumbHeight
	}
	thumbStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Primary)
	trackStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Border)

	var rows []string
	for i := startIdx; i < endIdx; i++ {
		u := m.users[m.filtered[i]]
		isHighlight := i == m.highlight

		row := m.renderRow(u, contentWidth, isHighlight, bg)
		if showScrollbar {
			rel := i - startIdx
			if rel >= thumbStart && rel < thumbEnd {
				row += thumbStyle.Render("\u2588")
			} else {
				row += trackStyle.Render("\u2502")
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// renderRow renders a single user row: display name, @handle, [ext]
// tag if external, and a trailing "✓" if the user is in the pill bar.
func (m Model) renderRow(u User, width int, highlight bool, bg lipgloss.Color) string {
	name := u.DisplayName
	if u.IsExternal {
		name += " [ext]"
	}

	handle := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render(" @" + u.Username)

	var nameStyle lipgloss.Style
	if highlight {
		nameStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.Primary).Bold(true)
	} else {
		nameStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
	}
	nameRendered := nameStyle.Render(name)

	check := ""
	if _, sel := m.selected[u.ID]; sel {
		check = lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render(" ✓")
	}

	line := nameRendered + handle + check
	if lipgloss.Width(line) > width {
		line = truncate.StringWithTail(line, uint(width), "…")
	}
	if pad := width - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}

	if highlight {
		indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("▌")
		return indicator + line
	}
	return " " + line
}

// renderFooter is the key-hints + N/8 counter row.
func (m Model) renderFooter(innerWidth int, bg lipgloss.Color) string {
	left := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render("space toggle  enter open  esc cancel")

	counterText := fmt.Sprintf("%d / %d", len(m.selected), MaxRecipients)
	var counterStyle lipgloss.Style
	if len(m.selected) >= MaxRecipients {
		counterStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Italic(true)
		counterText += "  MPIM limit reached"
	} else {
		counterStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted)
	}
	right := counterStyle.Render(counterText)

	gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
```

- [ ] **Step 2: Run the package tests to confirm the view code compiles and existing tests still pass**

```
go test ./internal/ui/newmessagepicker/ -v
```

Expected: all 31 tests PASS. (Compilation alone catches typos in the imports.)

- [ ] **Step 3: Run a smoke render via `go vet`**

```
go vet ./internal/ui/newmessagepicker/
```

Expected: no output (no issues).

- [ ] **Step 4: Commit**

```
git add internal/ui/newmessagepicker/view.go
git commit -m "feat(new-message): render the picker modal

Title, pill bar + query input, scrollable list with [ext] tag for
externals and a ✓ for selected users, scrollbar when overflowing,
footer with key hints and N/8 counter. Uses overlay.DimmedOverlay
for the centered modal composition like channelfinder."
```
