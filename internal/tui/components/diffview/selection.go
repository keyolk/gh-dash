package diffview

// Cursor / selection state for visual-block (and visual-line) modes.

// SelectMode is the active selection mode.
type SelectMode int

const (
	SelectNone SelectMode = iota
	SelectLine               // line-wise (vim `V`)
	SelectBlock              // block-wise (vim `Ctrl-V`)
)

// CodeRef points to one renderable code line inside the diff (i.e. one of
// the lines you can put a comment on). It carries enough information to
// later build a GitHub review comment payload.
type CodeRef struct {
	FileIndex int
	HunkIndex int
	LineIndex int // index inside Hunk.Lines

	// Convenience copies so consumers don't need the parent slice.
	Path string
	Kind LineKind
	Old  int
	New  int
}

// Selection represents the active (possibly empty) range selection.
type Selection struct {
	Mode       SelectMode
	AnchorRow  int // displayable-row index where selection started
	CursorRow  int // current cursor
	AnchorCol  int // block-mode anchor column
	CursorCol  int // block-mode cursor column
}

// IsActive reports whether a non-empty selection exists.
func (s Selection) IsActive() bool { return s.Mode != SelectNone }

// Range returns the normalised [low, high] row indices and (for block mode)
// columns. Both ends are inclusive.
func (s Selection) Range() (rowLo, rowHi, colLo, colHi int) {
	rowLo, rowHi = s.AnchorRow, s.CursorRow
	if rowHi < rowLo {
		rowLo, rowHi = rowHi, rowLo
	}
	colLo, colHi = s.AnchorCol, s.CursorCol
	if colHi < colLo {
		colLo, colHi = colHi, colLo
	}
	return
}
