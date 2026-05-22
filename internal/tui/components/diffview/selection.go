package diffview

// Cursor / selection state for visual-block (and visual-line) modes.

// SelectMode is the active selection mode.
type SelectMode int

const (
	SelectNone  SelectMode = iota
	SelectLine             // line-wise (vim `V`)
	SelectBlock            // block-wise (vim `Ctrl-V`)
)

// Side indicates which half of a side-by-side render the cursor is on. The
// inline mode always uses SideRight conceptually but doesn't render the
// distinction.
type Side int

const (
	SideRight Side = iota // additions / context — new file
	SideLeft              // deletions / context — old file
)

func (s Side) String() string {
	if s == SideLeft {
		return "LEFT"
	}
	return "RIGHT"
}

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
	Mode      SelectMode
	AnchorRow int // displayable-row index where selection started
	CursorRow int // current cursor
	Side      Side
}

// IsActive reports whether a non-empty selection exists.
func (s Selection) IsActive() bool { return s.Mode != SelectNone }

// Range returns the normalised [low, high] row indices. Both ends inclusive.
func (s Selection) Range() (rowLo, rowHi int) {
	rowLo, rowHi = s.AnchorRow, s.CursorRow
	if rowHi < rowLo {
		rowLo, rowHi = rowHi, rowLo
	}
	return
}
