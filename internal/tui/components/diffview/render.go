package diffview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	// Backgrounds per line kind when not selected / cursored.
	addBg = lipgloss.Color("22") // dark green
	delBg = lipgloss.Color("52") // dark red

	addFg = lipgloss.Color("10")
	delFg = lipgloss.Color("9")

	// Highlight backgrounds — picked to be unmistakable atop add/del bg.
	cursorBg = lipgloss.Color("226") // bright yellow
	cursorFg = lipgloss.Color("0")   // black
	selectBg = lipgloss.Color("201") // bright magenta
	selectFg = lipgloss.Color("15")  // white
	dimBg    = lipgloss.Color("237") // dark grey for the inactive half
	dimFg    = lipgloss.Color("245")

	dividerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	fileHdrStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	hunkHdrStyle  = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("13"))
	commentMarker = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("💬")
)

// rowKind classifies a rendered row so we know whether it's selectable.
type rowKind int

const (
	rowFileHeader rowKind = iota
	rowHunkHeader
	rowCode
)

// row is one displayable line. For code rows we store plain (ANSI-free)
// text and the kind of each half so stringify can pick a final background
// in one pass without fighting inner ANSI resets.
type row struct {
	kind       rowKind
	headerText string // pre-styled text for non-code rows

	plainLeft  string
	plainRight string
	leftKind   LineKind
	rightKind  LineKind
	hasLeft    bool
	hasRight   bool

	ref      *CodeRef // primary ref (right side preferred, falls back to left)
	leftRef  *CodeRef
	rightRef *CodeRef
}

// renderedDoc is the result of buildDoc, ready for highlight-aware
// stringification.
type renderedDoc struct {
	rows     []row
	mode     Mode
	width    int
	colWidth int
}

// buildDoc lays out the diff into rows + per-half metadata.
func buildDoc(files []File, width int, mode Mode) renderedDoc {
	doc := renderedDoc{mode: mode, width: width}
	if mode == ModeSideBySide {
		// Reserve 1 cell for the divider; split the remainder evenly. If
		// `width` is even we deliberately lose one cell to the right column
		// instead of overshooting the viewport width and triggering wrap.
		doc.colWidth = (width - 1) / 2
		if doc.colWidth < 10 {
			doc.colWidth = 10
		}
	}

	for fi, f := range files {
		doc.rows = append(doc.rows, row{
			kind:       rowFileHeader,
			headerText: fileHdrStyle.Render(fmt.Sprintf("▸ %s", f.Path())),
		})
		for hi, h := range f.Hunks {
			doc.rows = append(doc.rows, row{
				kind: rowHunkHeader,
				headerText: hunkHdrStyle.Render(fmt.Sprintf(
					"  @@ -%d,%d +%d,%d @@ %s",
					h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Header,
				)),
			})

			if mode == ModeInline {
				for li, ln := range h.Lines {
					ref := codeRef(f.Path(), fi, hi, li, ln)
					doc.rows = append(doc.rows, row{
						kind:      rowCode,
						plainLeft: plainInlineLine(ln, width),
						leftKind:  ln.Kind,
						hasLeft:   true,
						ref:       ref,
					})
				}
				continue
			}

			pairs := pairSideBySideWithIndex(h.Lines)
			for _, p := range pairs {
				var lRef, rRef *CodeRef
				var lTxt, rTxt string
				var lKind, rKind LineKind
				hasL, hasR := false, false
				if p.left != nil {
					lRef = codeRef(f.Path(), fi, hi, p.leftIdx, *p.left)
					lTxt = plainHalfRow(p.left, doc.colWidth, true)
					lKind = p.left.Kind
					hasL = true
				} else {
					lTxt = strings.Repeat(" ", doc.colWidth)
				}
				if p.right != nil {
					rRef = codeRef(f.Path(), fi, hi, p.rightIdx, *p.right)
					rTxt = plainHalfRow(p.right, doc.colWidth, false)
					rKind = p.right.Kind
					hasR = true
				} else {
					rTxt = strings.Repeat(" ", doc.colWidth)
				}
				primary := rRef
				if primary == nil {
					primary = lRef
				}
				doc.rows = append(doc.rows, row{
					kind:       rowCode,
					plainLeft:  lTxt,
					plainRight: rTxt,
					leftKind:   lKind,
					rightKind:  rKind,
					hasLeft:    hasL,
					hasRight:   hasR,
					ref:        primary,
					leftRef:    lRef,
					rightRef:   rRef,
				})
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

// highlightState captures the "extra" colouring on top of the per-kind bg.
type highlightState int

const (
	hlNone highlightState = iota
	hlSelectedDim
	hlSelected
	hlCursor
)

// halfStyle returns the lipgloss style to render a half-row. The state arg
// wins over the per-kind colouring so a selected or cursored line is
// visually unambiguous regardless of whether it's an add / del / context.
func halfStyle(kind LineKind, hasContent bool, state highlightState) lipgloss.Style {
	s := lipgloss.NewStyle()
	if !hasContent {
		// Padding placeholder — keep the dim background when selected so the
		// covered range is still visible on the inactive side.
		if state == hlSelectedDim {
			return s.Background(dimBg)
		}
		return s
	}
	switch state {
	case hlCursor:
		return s.Background(cursorBg).Foreground(cursorFg).Bold(true)
	case hlSelected:
		return s.Background(selectBg).Foreground(selectFg).Bold(true)
	case hlSelectedDim:
		return s.Background(dimBg).Foreground(dimFg)
	}
	switch kind {
	case LineAdd:
		return s.Background(addBg).Foreground(addFg)
	case LineDel:
		return s.Background(delBg).Foreground(delFg)
	}
	return s
}

// stringify produces the final viewport content with cursor / selection
// highlights applied.
func (d renderedDoc) stringify(sel Selection, cursorRow int, activeSide Side, comments map[CodeRef]bool) string {
	var b strings.Builder
	for i, r := range d.rows {
		if r.kind != rowCode {
			b.WriteString(r.headerText)
			b.WriteString("\n")
			continue
		}

		leftState, rightState := hlNone, hlNone
		isCursor := i == cursorRow
		inSel := false
		selSide := activeSide
		if sel.IsActive() {
			lo, hi := sel.Range()
			if i >= lo && i <= hi {
				inSel = true
				selSide = sel.Side
			}
		}

		if d.mode == ModeSideBySide {
			if inSel {
				if selSide == SideLeft {
					leftState, rightState = hlSelected, hlSelectedDim
				} else {
					rightState, leftState = hlSelected, hlSelectedDim
				}
			}
			if isCursor {
				if activeSide == SideLeft {
					leftState = hlCursor
				} else {
					rightState = hlCursor
				}
			}
		} else {
			if inSel {
				leftState = hlSelected
			}
			if isCursor {
				leftState = hlCursor
			}
		}

		leftStyled := halfStyle(r.leftKind, r.hasLeft, leftState).Render(r.plainLeft)
		var line string
		if d.mode == ModeSideBySide {
			rightStyled := halfStyle(r.rightKind, r.hasRight, rightState).Render(r.plainRight)
			line = leftStyled + dividerStyle.Render("│") + rightStyled
		} else {
			line = leftStyled
		}

		if r.ref != nil && comments[*r.ref] {
			line = appendMarker(line, d.width, commentMarker)
		}

		// Hard-cap the composed row to width-1 cells. The 1-cell margin
		// absorbs any drift between our lipgloss.Width measurements (which
		// the embedded viewport uses to decide whether to wrap) and what
		// the terminal actually renders — long lines with mixed-width
		// glyphs or ellipsis can otherwise tip a row one cell over and
		// trigger wrap.
		line = capDisplay(line, d.width-1)

		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// renderInline / renderSideBySide are kept as the public entry points for
// non-stateful rendering (tests, snapshots).
func renderInline(files []File, width int) string {
	return buildDoc(files, width, ModeInline).stringify(Selection{}, -1, SideRight, nil)
}

func renderSideBySide(files []File, width int) string {
	return buildDoc(files, width, ModeSideBySide).stringify(Selection{}, -1, SideRight, nil)
}

// plainInlineLine builds the ANSI-free row used in inline mode.
func plainInlineLine(ln Line, width int) string {
	gOld := formatLineNum(ln.OldNum)
	gNew := formatLineNum(ln.NewNum)
	marker := " "
	switch ln.Kind {
	case LineAdd:
		marker = "+"
	case LineDel:
		marker = "-"
	case LineNoNewline:
		marker = "\\"
	}
	prefix := fmt.Sprintf("%s %s %s ", gOld, gNew, marker)
	maxText := width - lipgloss.Width(prefix)
	if maxText < 1 {
		maxText = 1
	}
	text := truncate(expandTabs(ln.Text), maxText)
	line := prefix + text
	return fitWidth(line, width)
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

// plainHalfRow builds an ANSI-free half-row used in side-by-side mode.
func plainHalfRow(ln *Line, colWidth int, isLeft bool) string {
	if ln == nil {
		return strings.Repeat(" ", colWidth)
	}
	var num int
	if isLeft {
		num = ln.OldNum
	} else {
		num = ln.NewNum
	}
	marker := " "
	switch ln.Kind {
	case LineAdd:
		marker = "+"
	case LineDel:
		marker = "-"
	case LineNoNewline:
		marker = "\\"
	}
	prefix := fmt.Sprintf("%s %s ", formatLineNum(num), marker)
	maxText := colWidth - lipgloss.Width(prefix)
	if maxText < 1 {
		maxText = 1
	}
	text := truncate(expandTabs(ln.Text), maxText)
	return fitWidth(prefix+text, colWidth)
}

// fitWidth pads `s` to exactly `width` display cells. If `s` is already
// wider it is hard-truncated (no ellipsis) so it can't wrap to the next
// row in the viewport.
func fitWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w == width {
		return s
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	// Hard cut to display width — protects against any prefix/measure mismatch.
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// capDisplay is fitWidth's truncate-only sibling: it never adds padding (so
// it preserves trailing ANSI resets coming out of styled segments) but does
// hard-cut content wider than `width`. Used for fully composed rows where
// padding has already been baked into each half.
func capDisplay(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func appendMarker(line string, width int, marker string) string {
	mWidth := lipgloss.Width(marker)
	if lipgloss.Width(line)+mWidth+1 > width {
		runes := []rune(line)
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
