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

// row is one displayable diff line. For code rows we store plain (ANSI-free)
// chunks of either half so stringify can pick a final background in one
// pass. Long content in either half is split into colWidth-cell chunks
// up-front: the row keeps the chunk list per side and stringify renders
// the chunks as multiple physical rows, padding the shorter side out with
// blank chunks so things stay aligned.
type row struct {
	kind       rowKind
	headerText string // pre-styled text for non-code rows

	leftChunks  []string // plain (ANSI-free) chunks, each colWidth/width wide
	rightChunks []string
	leftKind    LineKind
	rightKind   LineKind
	hasLeft     bool
	hasRight    bool

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
					chunks := buildInlineChunks(ln, width)
					doc.rows = append(doc.rows, row{
						kind:       rowCode,
						leftChunks: chunks,
						leftKind:   ln.Kind,
						hasLeft:    true,
						ref:        ref,
					})
				}
				continue
			}

			pairs := pairSideBySideWithIndex(h.Lines)
			for _, p := range pairs {
				var lRef, rRef *CodeRef
				var lChunks, rChunks []string
				var lKind, rKind LineKind
				hasL, hasR := false, false
				if p.left != nil {
					lRef = codeRef(f.Path(), fi, hi, p.leftIdx, *p.left)
					lChunks = buildHalfChunks(p.left, doc.colWidth, true)
					lKind = p.left.Kind
					hasL = true
				}
				if p.right != nil {
					rRef = codeRef(f.Path(), fi, hi, p.rightIdx, *p.right)
					rChunks = buildHalfChunks(p.right, doc.colWidth, false)
					rKind = p.right.Kind
					hasR = true
				}
				primary := rRef
				if primary == nil {
					primary = lRef
				}
				doc.rows = append(doc.rows, row{
					kind:        rowCode,
					leftChunks:  lChunks,
					rightChunks: rChunks,
					leftKind:    lKind,
					rightKind:   rKind,
					hasLeft:     hasL,
					hasRight:    hasR,
					ref:         primary,
					leftRef:     lRef,
					rightRef:    rRef,
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
// highlights applied. Each diff "row" may expand into multiple visual rows
// if either half wraps; the cursor and selection refer to the logical row,
// so every visual row produced by a selected logical row gets the same
// highlight.
func (d renderedDoc) stringify(sel Selection, cursorRow int, activeSide Side, comments map[CodeRef]bool) string {
	var b strings.Builder
	for i, r := range d.rows {
		if r.kind != rowCode {
			b.WriteString(r.headerText)
			b.WriteString("\n")
			continue
		}

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

		leftStateBase, rightStateBase := hlNone, hlNone
		if d.mode == ModeSideBySide {
			if inSel {
				if selSide == SideLeft {
					leftStateBase, rightStateBase = hlSelected, hlSelectedDim
				} else {
					rightStateBase, leftStateBase = hlSelected, hlSelectedDim
				}
			}
		} else if inSel {
			leftStateBase = hlSelected
		}

		// Build a deterministic number of visual rows per logical row by
		// padding the shorter half with blank chunks.
		var nVisual int
		if d.mode == ModeSideBySide {
			if len(r.leftChunks) > nVisual {
				nVisual = len(r.leftChunks)
			}
			if len(r.rightChunks) > nVisual {
				nVisual = len(r.rightChunks)
			}
			if nVisual == 0 {
				nVisual = 1
			}
		} else {
			nVisual = len(r.leftChunks)
			if nVisual == 0 {
				nVisual = 1
			}
		}

		for vi := 0; vi < nVisual; vi++ {
			leftState := leftStateBase
			rightState := rightStateBase
			// Cursor only highlights the first visual row of the logical row
			// to keep the "current line" intuition.
			if isCursor && vi == 0 {
				if d.mode == ModeSideBySide {
					if activeSide == SideLeft {
						leftState = hlCursor
					} else {
						rightState = hlCursor
					}
				} else {
					leftState = hlCursor
				}
			}

			var leftCell, rightCell string
			if d.mode == ModeSideBySide {
				leftCell = pickChunk(r.leftChunks, vi, d.colWidth)
				rightCell = pickChunk(r.rightChunks, vi, d.colWidth)
				leftStyled := halfStyle(r.leftKind, r.hasLeft, leftState).Render(leftCell)
				rightStyled := halfStyle(r.rightKind, r.hasRight, rightState).Render(rightCell)
				line := leftStyled + dividerStyle.Render("│") + rightStyled

				// On the first visual row, append the comment marker if any.
				if vi == 0 && r.ref != nil && comments[*r.ref] {
					line = appendMarker(line, d.width, commentMarker)
				}
				b.WriteString(capDisplay(line, d.width-1))
				b.WriteString("\n")
				continue
			}

			leftCell = pickChunk(r.leftChunks, vi, d.width)
			line := halfStyle(r.leftKind, r.hasLeft, leftState).Render(leftCell)
			if vi == 0 && r.ref != nil && comments[*r.ref] {
				line = appendMarker(line, d.width, commentMarker)
			}
			b.WriteString(capDisplay(line, d.width-1))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// pickChunk returns the visual-row-`i` chunk (or a blank pad of width `w`).
func pickChunk(chunks []string, i, w int) string {
	if i < len(chunks) {
		return chunks[i]
	}
	return strings.Repeat(" ", w)
}

// visualHeight returns the number of physical viewport rows that logical
// row `i` occupies after wrapping. Non-code rows always take one row.
func (d renderedDoc) visualHeight(i int) int {
	if i < 0 || i >= len(d.rows) {
		return 0
	}
	r := d.rows[i]
	if r.kind != rowCode {
		return 1
	}
	if d.mode == ModeSideBySide {
		n := len(r.leftChunks)
		if len(r.rightChunks) > n {
			n = len(r.rightChunks)
		}
		if n == 0 {
			n = 1
		}
		return n
	}
	n := len(r.leftChunks)
	if n == 0 {
		n = 1
	}
	return n
}

// renderInline / renderSideBySide are kept as the public entry points for
// non-stateful rendering (tests, snapshots).
func renderInline(files []File, width int) string {
	return buildDoc(files, width, ModeInline).stringify(Selection{}, -1, SideRight, nil)
}

func renderSideBySide(files []File, width int) string {
	return buildDoc(files, width, ModeSideBySide).stringify(Selection{}, -1, SideRight, nil)
}

// buildInlineChunks slices an inline-mode line into colWidth-cell chunks,
// keeping the gutter intact on the first chunk and indented spaces on
// continuation chunks so wrapped text lines up under the original.
func buildInlineChunks(ln Line, width int) []string {
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
	contPrefix := strings.Repeat(" ", lipgloss.Width(prefix))
	return wrapWithPrefixes(prefix, contPrefix, expandTabs(ln.Text), width)
}

// buildHalfChunks slices a side-by-side half-row into colWidth-cell chunks.
func buildHalfChunks(ln *Line, colWidth int, isLeft bool) []string {
	if ln == nil {
		return nil
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
	contPrefix := strings.Repeat(" ", lipgloss.Width(prefix))
	return wrapWithPrefixes(prefix, contPrefix, expandTabs(ln.Text), colWidth)
}

// wrapWithPrefixes wraps `text` into chunks of exactly `width` display cells.
// The first chunk uses `firstPrefix`; subsequent chunks use `contPrefix`.
// Every chunk is padded on the right to `width` so callers can stack them
// into visual rows without worrying about column drift.
func wrapWithPrefixes(firstPrefix, contPrefix, text string, width int) []string {
	if width < 4 {
		width = 4
	}
	var chunks []string
	textRunes := []rune(text)

	first := true
	for {
		var prefix string
		if first {
			prefix = firstPrefix
			first = false
		} else {
			prefix = contPrefix
		}
		room := width - lipgloss.Width(prefix)
		if room < 1 {
			// pathological — prefix doesn't fit; emit prefix alone padded.
			chunks = append(chunks, fitWidth(prefix, width))
			return chunks
		}
		if len(textRunes) == 0 {
			// Whole text consumed (or empty). Emit at least one chunk so the
			// gutter shows up for blank context lines.
			if len(chunks) == 0 {
				chunks = append(chunks, fitWidth(prefix, width))
			}
			return chunks
		}
		// take up to `room` cells worth of runes.
		take := 0
		acc := 0
		for take < len(textRunes) {
			w := runeWidth(textRunes[take])
			if acc+w > room {
				break
			}
			acc += w
			take++
		}
		if take == 0 {
			// A single rune wider than `room` — force-take it to avoid an
			// infinite loop. capDisplay later trims if needed.
			take = 1
		}
		chunk := prefix + string(textRunes[:take])
		chunks = append(chunks, fitWidth(chunk, width))
		textRunes = textRunes[take:]
	}
}

func runeWidth(r rune) int {
	// lipgloss.Width on a single-rune string is the cheapest accurate path
	// we have without pulling in another dep.
	return lipgloss.Width(string(r))
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
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// capDisplay hard-cuts a string to at most `width` cells, never padding.
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
