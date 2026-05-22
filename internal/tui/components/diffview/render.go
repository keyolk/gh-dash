package diffview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	gutterStyle    = lipgloss.NewStyle().Faint(true)
	addBgStyle     = lipgloss.NewStyle().Background(lipgloss.Color("22"))
	delBgStyle     = lipgloss.NewStyle().Background(lipgloss.Color("52"))
	addTextStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delTextStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	fileHdrStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	hunkHdrStyle   = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("13"))
	cursorBgStyle  = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	selectBgStyle  = lipgloss.NewStyle().Background(lipgloss.Color("24"))
	commentMarker  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("💬")
)

// rowKind classifies a rendered row so we know whether it's selectable.
type rowKind int

const (
	rowFileHeader rowKind = iota
	rowHunkHeader
	rowCode
)

// row is one displayable line in the rendered diff.
type row struct {
	kind   rowKind
	left   string // left-half raw text (inline mode uses this only)
	right  string // right-half raw text (side-by-side only)
	ref    *CodeRef
	leftRef  *CodeRef // for side-by-side: ref backing the left half
	rightRef *CodeRef // for side-by-side: ref backing the right half
}

// renderedDoc is the result of building either inline or side-by-side rows.
type renderedDoc struct {
	rows []row
	mode Mode
	// width / colWidth are kept so we can recompute highlights without
	// re-laying-out the full diff.
	width    int
	colWidth int
}

// buildDoc renders the diff into rows + per-row metadata. Highlights are
// applied separately in stringify().
func buildDoc(files []File, width int, mode Mode) renderedDoc {
	doc := renderedDoc{mode: mode, width: width}
	if mode == ModeSideBySide {
		doc.colWidth = (width - 1) / 2
		if doc.colWidth < 10 {
			doc.colWidth = 10
		}
	}

	for fi, f := range files {
		doc.rows = append(doc.rows, row{
			kind: rowFileHeader,
			left: fileHdrStyle.Render(fmt.Sprintf("▸ %s", f.Path())),
		})
		for hi, h := range f.Hunks {
			doc.rows = append(doc.rows, row{
				kind: rowHunkHeader,
				left: hunkHdrStyle.Render(fmt.Sprintf(
					"  @@ -%d,%d +%d,%d @@ %s",
					h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Header,
				)),
			})

			if mode == ModeInline {
				for li, ln := range h.Lines {
					ref := codeRef(f.Path(), fi, hi, li, ln)
					doc.rows = append(doc.rows, row{
						kind: rowCode,
						left: renderInlineLine(ln, width),
						ref:  ref,
					})
				}
			} else {
				pairs := pairSideBySideWithIndex(h.Lines)
				for _, p := range pairs {
					var lRef, rRef *CodeRef
					var lTxt, rTxt string
					if p.left != nil {
						lRef = codeRef(f.Path(), fi, hi, p.leftIdx, *p.left)
						lTxt = renderHalfRow(p.left, doc.colWidth, true)
					} else {
						lTxt = strings.Repeat(" ", doc.colWidth)
					}
					if p.right != nil {
						rRef = codeRef(f.Path(), fi, hi, p.rightIdx, *p.right)
						rTxt = renderHalfRow(p.right, doc.colWidth, false)
					} else {
						rTxt = strings.Repeat(" ", doc.colWidth)
					}
					// pick a primary ref: prefer the new-side, fall back to old.
					primary := rRef
					if primary == nil {
						primary = lRef
					}
					doc.rows = append(doc.rows, row{
						kind:     rowCode,
						left:     lTxt,
						right:    rTxt,
						ref:      primary,
						leftRef:  lRef,
						rightRef: rRef,
					})
				}
			}
		}
	}
	return doc
}

func codeRef(path string, fi, hi, li int, ln Line) *CodeRef {
	return &CodeRef{
		FileIndex: fi, HunkIndex: hi, LineIndex: li,
		Path: path, Kind: ln.Kind, Old: ln.OldNum, New: ln.NewNum,
	}
}

// stringify produces the final viewport content with cursor / selection
// highlights applied. Highlight passes are cheap because we work on rendered
// rows instead of re-laying-out the full diff.
func (d renderedDoc) stringify(sel Selection, cursorRow int, comments map[CodeRef]bool) string {
	var b strings.Builder
	for i, r := range d.rows {
		line := d.composeRow(r)

		// Decorate with comment marker on the right (truncating the line
		// slightly so the marker doesn't push past the row width).
		if r.kind == rowCode && r.ref != nil && comments[*r.ref] {
			line = appendMarker(line, d.width, commentMarker)
		}

		// Apply selection / cursor highlight.
		if r.kind == rowCode {
			if sel.IsActive() {
				lo, hi, _, _ := sel.Range()
				if i >= lo && i <= hi {
					line = selectBgStyle.Render(line)
				}
			}
			if i == cursorRow {
				line = cursorBgStyle.Render(line)
			}
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (d renderedDoc) composeRow(r row) string {
	if d.mode == ModeSideBySide && r.kind == rowCode {
		divider := gutterStyle.Render("│")
		return r.left + divider + r.right
	}
	return r.left
}

// renderInline / renderSideBySide are kept as the public entry points for
// non-stateful rendering (tests, snapshots).
func renderInline(files []File, width int) string {
	return buildDoc(files, width, ModeInline).stringify(Selection{}, -1, nil)
}

func renderSideBySide(files []File, width int) string {
	return buildDoc(files, width, ModeSideBySide).stringify(Selection{}, -1, nil)
}

func renderInlineLine(ln Line, width int) string {
	const gutterWidth = 6
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
	line := fmt.Sprintf("%s%s %s", gutter, marker, textStyle.Render(text))
	pad := width - lipgloss.Width(line)
	if pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return rowStyle.Render(line)
}

// indexedPair keeps the original Line index so callers can recover a CodeRef.
type indexedPair struct {
	left     *Line
	right    *Line
	leftIdx  int
	rightIdx int
}

func pairSideBySideWithIndex(lines []Line) []indexedPair {
	var out []indexedPair
	type pending struct {
		ln  Line
		idx int
	}
	var dels, adds []pending

	flush := func() {
		n := len(dels)
		if len(adds) > n {
			n = len(adds)
		}
		for i := 0; i < n; i++ {
			var p indexedPair
			if i < len(dels) {
				l := dels[i].ln
				p.left = &l
				p.leftIdx = dels[i].idx
			}
			if i < len(adds) {
				l := adds[i].ln
				p.right = &l
				p.rightIdx = adds[i].idx
			}
			out = append(out, p)
		}
		dels = dels[:0]
		adds = adds[:0]
	}

	for i, ln := range lines {
		switch ln.Kind {
		case LineDel:
			dels = append(dels, pending{ln, i})
		case LineAdd:
			adds = append(adds, pending{ln, i})
		case LineContext, LineNoNewline:
			flush()
			l := ln
			out = append(out, indexedPair{left: &l, right: &l, leftIdx: i, rightIdx: i})
		}
	}
	flush()
	return out
}

func renderHalfRow(ln *Line, colWidth int, isLeft bool) string {
	const gutterWidth = 5
	if ln == nil {
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
	line := fmt.Sprintf("%s%s %s", gutter, marker, textStyle.Render(text))
	pad := colWidth - lipgloss.Width(line)
	if pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return rowStyle.Render(line)
}

func appendMarker(line string, width int, marker string) string {
	mWidth := lipgloss.Width(marker)
	if lipgloss.Width(line)+mWidth+1 > width {
		// drop the trailing pad to make room for the marker
		runes := []rune(line)
		// crude trim from end while preserving styling: rebuild by truncating.
		for len(runes) > 0 && lipgloss.Width(string(runes))+mWidth+1 > width {
			runes = runes[:len(runes)-1]
		}
		line = string(runes)
	}
	return line + " " + marker
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
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i]) + "…"
		if lipgloss.Width(candidate) <= w {
			return candidate
		}
	}
	return ""
}
