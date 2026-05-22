package diffview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	gutterStyle = lipgloss.NewStyle().Faint(true)
	addBgStyle  = lipgloss.NewStyle().Background(lipgloss.Color("22"))
	delBgStyle  = lipgloss.NewStyle().Background(lipgloss.Color("52"))
	addTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	fileHdrStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	hunkHdrStyle = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("13"))
)

// renderInline renders a unified inline diff (one row per line).
func renderInline(files []File, width int) string {
	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fileHdrStyle.Render(fmt.Sprintf("▸ %s", f.Path())))
		b.WriteString("\n")
		for _, h := range f.Hunks {
			b.WriteString(hunkHdrStyle.Render(fmt.Sprintf(
				"  @@ -%d,%d +%d,%d @@ %s",
				h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Header,
			)))
			b.WriteString("\n")
			for _, ln := range h.Lines {
				b.WriteString(renderInlineLine(ln, width))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func renderInlineLine(ln Line, width int) string {
	const gutterWidth = 6 // " 1234 " on each side
	gOld := formatLineNum(ln.OldNum)
	gNew := formatLineNum(ln.NewNum)
	gutter := gutterStyle.Render(fmt.Sprintf("%s %s ", gOld, gNew))

	marker := " "
	textStyle := lipgloss.NewStyle()
	rowStyle := lipgloss.NewStyle()
	switch ln.Kind {
	case LineAdd:
		marker = "+"
		textStyle = addTextStyle
		rowStyle = addBgStyle
	case LineDel:
		marker = "-"
		textStyle = delTextStyle
		rowStyle = delBgStyle
	case LineNoNewline:
		marker = "\\"
		textStyle = lipgloss.NewStyle().Faint(true)
	}

	maxText := width - (gutterWidth*2 + 2)
	if maxText < 1 {
		maxText = 1
	}
	text := truncate(expandTabs(ln.Text), maxText)
	row := fmt.Sprintf("%s%s %s", gutter, marker, textStyle.Render(text))
	pad := width - lipgloss.Width(row)
	if pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	return rowStyle.Render(row)
}

// renderSideBySide produces a two-column view. For each hunk we pair add/del
// lines greedily so a `-foo` followed by a `+bar` shows side by side.
func renderSideBySide(files []File, width int) string {
	colWidth := (width - 1) / 2 // 1 column for the divider
	if colWidth < 10 {
		colWidth = 10
	}

	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fileHdrStyle.Render(fmt.Sprintf("▸ %s", f.Path())))
		b.WriteString("\n")
		for _, h := range f.Hunks {
			b.WriteString(hunkHdrStyle.Render(fmt.Sprintf(
				"  @@ -%d,%d +%d,%d @@ %s",
				h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Header,
			)))
			b.WriteString("\n")
			rows := pairSideBySide(h.Lines)
			for _, p := range rows {
				b.WriteString(renderSideBySideRow(p, colWidth))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// pairedRow holds the (optional) old-side and new-side lines for one display row.
type pairedRow struct {
	left  *Line // displayed on the left (old / del / context)
	right *Line // displayed on the right (new / add / context)
}

// pairSideBySide walks a hunk's lines and groups del/add pairs together while
// keeping context lines aligned in both columns.
func pairSideBySide(lines []Line) []pairedRow {
	var rows []pairedRow
	var pendingDels []Line
	var pendingAdds []Line

	flush := func() {
		n := len(pendingDels)
		if len(pendingAdds) > n {
			n = len(pendingAdds)
		}
		for i := 0; i < n; i++ {
			var pr pairedRow
			if i < len(pendingDels) {
				l := pendingDels[i]
				pr.left = &l
			}
			if i < len(pendingAdds) {
				l := pendingAdds[i]
				pr.right = &l
			}
			rows = append(rows, pr)
		}
		pendingDels = pendingDels[:0]
		pendingAdds = pendingAdds[:0]
	}

	for _, ln := range lines {
		switch ln.Kind {
		case LineDel:
			pendingDels = append(pendingDels, ln)
		case LineAdd:
			pendingAdds = append(pendingAdds, ln)
		case LineContext:
			flush()
			l := ln
			rows = append(rows, pairedRow{left: &l, right: &l})
		case LineNoNewline:
			flush()
			l := ln
			rows = append(rows, pairedRow{left: &l, right: &l})
		}
	}
	flush()
	return rows
}

func renderSideBySideRow(pr pairedRow, colWidth int) string {
	left := renderHalfRow(pr.left, colWidth, true)
	right := renderHalfRow(pr.right, colWidth, false)
	divider := gutterStyle.Render("│")
	return left + divider + right
}

func renderHalfRow(ln *Line, colWidth int, isLeft bool) string {
	const gutterWidth = 5
	if ln == nil {
		// empty pad for alignment
		row := strings.Repeat(" ", colWidth)
		return gutterStyle.Render(row)
	}
	var (
		num    int
		marker = " "
	)
	if isLeft {
		num = ln.OldNum
	} else {
		num = ln.NewNum
	}

	textStyle := lipgloss.NewStyle()
	rowStyle := lipgloss.NewStyle()
	switch ln.Kind {
	case LineAdd:
		marker = "+"
		textStyle = addTextStyle
		rowStyle = addBgStyle
	case LineDel:
		marker = "-"
		textStyle = delTextStyle
		rowStyle = delBgStyle
	case LineNoNewline:
		marker = "\\"
		textStyle = lipgloss.NewStyle().Faint(true)
	}
	gutter := gutterStyle.Render(fmt.Sprintf("%s ", formatLineNum(num)))
	maxText := colWidth - (gutterWidth + 2)
	if maxText < 1 {
		maxText = 1
	}
	text := truncate(expandTabs(ln.Text), maxText)
	row := fmt.Sprintf("%s%s %s", gutter, marker, textStyle.Render(text))
	pad := colWidth - lipgloss.Width(row)
	if pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	return rowStyle.Render(row)
}

func formatLineNum(n int) string {
	if n == 0 {
		return "    "
	}
	return fmt.Sprintf("%4d", n)
}

func expandTabs(s string) string {
	return strings.ReplaceAll(s, "\t", "    ")
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	// Naive byte truncate is OK here — gh diffs are utf-8 but rarely have
	// wide runes in code. lipgloss.Width handles the display measurement.
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i]) + "…"
		if lipgloss.Width(candidate) <= w {
			return candidate
		}
	}
	return ""
}
