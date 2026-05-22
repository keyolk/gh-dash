package diffview

import (
	"fmt"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	log "charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

// Mode is the visual layout of the diff.
type Mode int

const (
	ModeInline Mode = iota
	ModeSideBySide
)

// Loaded is sent when an async fetch of `gh pr diff` completes.
type Loaded struct {
	PRNumber int
	Repo     string
	Files    []File
	Err      error
}

// Model is the bubbletea model for the in-app diff viewer.
type Model struct {
	ctx      *context.ProgramContext
	IsOpen   bool
	Loading  bool
	PRNumber int
	Repo     string
	Title    string
	mode     Mode
	files    []File
	err      error
	viewport viewport.Model
	doc      renderedDoc

	cursorRow int
	sel       Selection
	codeRows  []int
	comments  map[CodeRef]bool
	// side is the currently-active half in side-by-side mode (LEFT vs
	// RIGHT). Inline mode pins this to SideRight.
	side Side
	// helpVisible toggles a help overlay listing diff-viewer key bindings.
	helpVisible bool

	pending []PendingComment
	editor  commentEditor

	// existing holds comments fetched from GitHub for this PR.
	existing []ExistingComment
}

// NewModel constructs an empty diff viewer.
func NewModel(ctx *context.ProgramContext) Model {
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	return Model{
		ctx:      ctx,
		viewport: vp,
		mode:     ModeSideBySide,
		editor:   newCommentEditor(),
	}
}

// Open starts an async fetch of the diff and shows the (loading) viewer.
func (m *Model) Open(prNumber int, repo, title string) tea.Cmd {
	m.IsOpen = true
	m.Loading = true
	m.PRNumber = prNumber
	m.Repo = repo
	m.Title = title
	m.files = nil
	m.err = nil
	m.doc = renderedDoc{}
	m.codeRows = nil
	m.cursorRow = -1
	m.sel = Selection{}
	m.pending = nil
	m.existing = nil
	if m.comments == nil {
		m.comments = map[CodeRef]bool{}
	} else {
		for k := range m.comments {
			delete(m.comments, k)
		}
	}
	m.viewport.SetContent("")
	m.viewport.GotoTop()

	loadDiff := func() tea.Msg {
		out, err := exec.Command("gh", "pr", "diff", fmt.Sprint(prNumber), "-R", repo).Output()
		if err != nil {
			return Loaded{PRNumber: prNumber, Repo: repo, Err: err}
		}
		files, perr := ParseUnified(string(out))
		if perr != nil {
			return Loaded{PRNumber: prNumber, Repo: repo, Err: perr}
		}
		return Loaded{PRNumber: prNumber, Repo: repo, Files: files}
	}
	return tea.Batch(loadDiff, fetchExistingComments(prNumber, repo))
}

// Close hides the diff viewer and clears its content.
func (m *Model) Close() {
	m.IsOpen = false
	m.Loading = false
	m.files = nil
	m.err = nil
	m.doc = renderedDoc{}
	m.codeRows = nil
	m.cursorRow = -1
	m.sel = Selection{}
	m.viewport.SetContent("")
}

// Mode returns the current layout mode.
func (m *Model) Mode() Mode { return m.mode }

// ToggleMode swaps between inline and side-by-side layouts.
func (m *Model) ToggleMode() {
	if m.mode == ModeInline {
		m.mode = ModeSideBySide
	} else {
		m.mode = ModeInline
		// inline view has no meaningful left/right distinction.
		m.side = SideRight
	}
	m.rebuild()
}

// ActiveSide reports which half of the side-by-side render the cursor sits on.
// Inline mode always reports SideRight.
func (m *Model) ActiveSide() Side { return m.side }

// ToggleSide swaps the active half in side-by-side mode. No-op in inline.
func (m *Model) ToggleSide() {
	if m.mode != ModeSideBySide {
		return
	}
	if m.side == SideRight {
		m.side = SideLeft
	} else {
		m.side = SideRight
	}
	if m.sel.IsActive() {
		m.sel.Side = m.side
	}
	m.refreshViewport()
}

// SetSide forces the active half to a specific side.
func (m *Model) SetSide(s Side) {
	if m.mode != ModeSideBySide {
		return
	}
	if m.side == s {
		return
	}
	m.side = s
	if m.sel.IsActive() {
		m.sel.Side = s
	}
	m.refreshViewport()
}

// HelpVisible reports whether the in-viewer help overlay is showing.
func (m *Model) HelpVisible() bool { return m.helpVisible }

// ToggleHelp shows / hides the diff viewer's own help overlay.
func (m *Model) ToggleHelp() {
	m.helpVisible = !m.helpVisible
	m.refreshViewport()
}

// UpdateProgramContext refreshes the viewport dimensions from the program context.
func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	if ctx == nil {
		return
	}
	m.ctx = ctx
	w, h := m.size()
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(h)
	m.rebuild()
}

// HandleLoaded folds a Loaded result into the model and (re)renders the diff.
func (m *Model) HandleLoaded(msg Loaded) {
	if msg.PRNumber != m.PRNumber || msg.Repo != m.Repo {
		return
	}
	m.Loading = false
	if msg.Err != nil {
		m.err = msg.Err
		log.Warn("diff load failed", "pr", msg.PRNumber, "err", msg.Err)
		return
	}
	m.files = msg.Files
	m.rebuild()
	m.viewport.GotoTop()
}

// Update routes navigation keys through the embedded viewport. The caller is
// expected to filter the messages: anything that should _not_ scroll the
// diff (mode toggle, close, comment shortcuts, etc.) must be handled before
// invoking Update.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.IsOpen {
		return m, nil
	}
	if m.editor.active {
		cmd := m.editor.update(msg)
		return m, cmd
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "j", "down":
			m.moveCursor(1)
			m.ensureCursorVisible()
			return m, nil
		case "k", "up":
			m.moveCursor(-1)
			m.ensureCursorVisible()
			return m, nil
		case "g", "home":
			if len(m.codeRows) > 0 {
				m.cursorRow = m.codeRows[0]
				if m.sel.Mode != SelectNone {
					m.sel.CursorRow = m.cursorRow
				}
				m.viewport.GotoTop()
				m.refreshViewport()
			}
			return m, nil
		case "G", "end":
			if len(m.codeRows) > 0 {
				m.cursorRow = m.codeRows[len(m.codeRows)-1]
				if m.sel.Mode != SelectNone {
					m.sel.CursorRow = m.cursorRow
				}
				m.viewport.GotoBottom()
				m.refreshViewport()
			}
			return m, nil
		case "V":
			m.StartSelection(SelectLine)
			return m, nil
		case "ctrl+v":
			m.StartSelection(SelectBlock)
			return m, nil
		case "h", "left":
			if m.mode == ModeSideBySide {
				m.SetSide(SideLeft)
				return m, nil
			}
		case "l", "right":
			if m.mode == ModeSideBySide {
				m.SetSide(SideRight)
				return m, nil
			}
		case "?":
			m.ToggleHelp()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// EditorActive reports whether the comment editor modal is open.
func (m *Model) EditorActive() bool { return m.editor.active }

// StartComment opens the comment editor against the current selection (or
// just the cursor row if no selection is active).
func (m *Model) StartComment() {
	refs := m.SelectionRefs()
	if len(refs) == 0 {
		return
	}
	width, _ := m.size()
	m.editor.open(refs, width)
}

// SaveComment finalises the editor into a PendingComment and refreshes
// markers. Returns true when a comment was actually appended.
func (m *Model) SaveComment() bool {
	if !m.editor.active {
		return false
	}
	body := m.editor.value()
	if body == "" {
		// empty body — treat as cancel
		m.editor.cancel()
		return false
	}
	pc := buildPending(m.editor.targets, body)
	m.editor.cancel()
	if pc.Path == "" {
		return false
	}
	m.pending = append(m.pending, pc)
	// mark every target ref so the comment marker shows up
	for _, ref := range m.editor.targets {
		m.comments[ref] = true
	}
	m.ClearSelection()
	m.refreshViewport()
	return true
}

// CancelComment discards the in-progress editor without saving.
func (m *Model) CancelComment() {
	m.editor.cancel()
}

// PendingComments returns a copy of the pending comments for inspection /
// later submission.
func (m *Model) PendingComments() []PendingComment {
	out := make([]PendingComment, len(m.pending))
	copy(out, m.pending)
	return out
}

// HandleCommentsFetched merges existing review comments into the viewer and
// rerenders so the markers light up. Called by the top-level UI when a
// CommentsFetched message arrives.
func (m *Model) HandleCommentsFetched(msg CommentsFetched) {
	if msg.PRNumber != m.PRNumber || msg.Repo != m.Repo {
		return
	}
	if msg.Err != nil {
		log.Warn("fetch existing comments failed", "pr", msg.PRNumber, "err", msg.Err)
		return
	}
	m.existing = msg.Comments
	m.applyExistingMarkers()
	m.refreshViewport()
}

// applyExistingMarkers walks the rendered code rows and turns on the comment
// marker for any row whose (path, line, side) matches an existing comment.
func (m *Model) applyExistingMarkers() {
	if len(m.existing) == 0 || len(m.doc.rows) == 0 {
		return
	}
	for _, r := range m.doc.rows {
		if r.kind != rowCode || r.ref == nil {
			continue
		}
		for _, ec := range m.existing {
			if ec.Path != r.ref.Path {
				continue
			}
			if matchesLine(ec, *r.ref) {
				m.comments[*r.ref] = true
				break
			}
		}
	}
}

func matchesLine(ec ExistingComment, ref CodeRef) bool {
	// `line` always set; `start_line` set only for multi-line. We mark
	// every code row falling in that range.
	if ec.Side == "LEFT" {
		if ref.Old == 0 {
			return false
		}
		if ec.StartLine != 0 {
			return ref.Old >= ec.StartLine && ref.Old <= ec.Line
		}
		return ref.Old == ec.Line
	}
	if ref.New == 0 {
		return false
	}
	if ec.StartLine != 0 {
		return ref.New >= ec.StartLine && ref.New <= ec.Line
	}
	return ref.New == ec.Line
}

// SubmitReview posts the pending comments as a single review.
// event must be one of "COMMENT", "APPROVE", "REQUEST_CHANGES".
func (m *Model) SubmitReview(event, body string) tea.Cmd {
	if len(m.pending) == 0 && event == "COMMENT" && body == "" {
		return nil
	}
	pending := make([]PendingComment, len(m.pending))
	copy(pending, m.pending)
	return submitReview(m.PRNumber, m.Repo, event, body, pending)
}

// HandleReviewSubmitted folds the result of a submitReview call back in.
// On success the pending queue is cleared and existing comments are
// re-fetched so the just-posted review shows up.
func (m *Model) HandleReviewSubmitted(msg ReviewSubmitted) tea.Cmd {
	if msg.PRNumber != m.PRNumber || msg.Repo != m.Repo {
		return nil
	}
	if msg.Err != nil {
		m.err = msg.Err
		log.Warn("submit review failed", "pr", msg.PRNumber, "err", msg.Err)
		m.refreshViewport()
		return nil
	}
	m.pending = nil
	// Re-fetch so freshly posted comments are visible.
	return fetchExistingComments(m.PRNumber, m.Repo)
}

// ensureCursorVisible scrolls the viewport so the cursor row stays inside
// the visible window.
func (m *Model) ensureCursorVisible() {
	if m.cursorRow < 0 {
		return
	}
	top := m.viewport.YOffset()
	h := m.viewport.Height()
	if h <= 0 {
		return
	}
	if m.cursorRow < top {
		m.viewport.SetYOffset(m.cursorRow)
	} else if m.cursorRow >= top+h {
		m.viewport.SetYOffset(m.cursorRow - h + 1)
	}
}

// View renders the diff overlay.
func (m Model) View() string {
	if !m.IsOpen {
		return ""
	}
	width, height := m.size()
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder())

	header := m.headerView(width)
	body := m.viewport.View()
	if m.Loading {
		body = lipgloss.PlaceVertical(height-2, lipgloss.Center,
			lipgloss.PlaceHorizontal(width, lipgloss.Center, "loading diff…"))
	}
	if m.err != nil {
		body = lipgloss.PlaceVertical(height-2, lipgloss.Center,
			lipgloss.PlaceHorizontal(width, lipgloss.Center,
				lipgloss.NewStyle().Foreground(lipgloss.Color("9")).
					Render(fmt.Sprintf("error: %v", m.err))))
	}
	footer := m.footerView(width)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
}

// HelpView returns the help overlay (or "" when hidden). Composed by the
// top-level UI as its own layer.
func (m Model) HelpView() string {
	if !m.helpVisible {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Background(lipgloss.Color("0")).
		Padding(0, 1)
	title := lipgloss.NewStyle().Bold(true).Render("Diff viewer keys")
	rows := [][2]string{
		{"j/k or ↓/↑", "cursor down / up"},
		{"g / G", "go to top / bottom"},
		{"ctrl+d / ctrl+u", "half-page down / up"},
		{"tab", "toggle inline / side-by-side"},
		{"h / l (←/→)", "switch active side (side-by-side)"},
		{"V", "visual-line selection"},
		{"ctrl+v", "visual-block selection"},
		{"esc", "clear selection (then close)"},
		{"c", "comment on cursor / selection"},
		{"ctrl+s / esc", "save / cancel comment editor"},
		{"R / A / X", "submit review (comment / approve / request)"},
		{"?", "toggle this help"},
		{"q", "close diff"},
	}
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	var lines []string
	lines = append(lines, title, "")
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("%s  %s", keyStyle.Render(padRight(r[0], 18)), r[1]))
	}
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// HelpPosition reports the top-left coordinates at which HelpView should be
// composited. We centre it within the diff frame so the rest of the diff
// stays visible.
func (m Model) HelpPosition() (int, int) {
	w, h := m.size()
	bw, bh := m.helpDimensions()
	x := max(0, (w-bw)/2)
	y := max(0, (h-bh)/2)
	return x, y
}

func (m Model) helpDimensions() (int, int) {
	// width ≈ 18 (key col) + 2 (gap) + max desc width (~48) + 4 padding/border
	return 72, 17
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// EditorView returns the editor overlay (or "" when inactive). The top-level
// UI composes this as its own layer so it can sit above the diff frame.
func (m Model) EditorView() string {
	if !m.editor.active {
		return ""
	}
	w, h := m.size()
	return m.editor.view(w, h)
}

// EditorPosition reports the top-left coordinates at which EditorView should
// be composited (centred within the diff frame).
func (m Model) EditorPosition() (int, int) {
	w, h := m.size()
	bw, bh := 64, 10
	x := max(0, (w-bw)/2)
	y := max(0, (h-bh)/2)
	return x, y
}

// MatchKey reports whether the given key string matches a binding by string
// comparison, falling back to Matches for chord forms. Kept tiny so callers
// don't need to import bubbles/key here.
func MatchKey(msg tea.KeyMsg, b key.Binding) bool { return key.Matches(msg, b) }

func (m Model) size() (int, int) {
	if m.ctx == nil {
		return 80, 24
	}
	w := m.ctx.ScreenWidth
	if w <= 0 {
		w = m.ctx.MainContentWidth
	}
	h := m.ctx.ScreenHeight
	if h <= 0 {
		h = m.ctx.MainContentHeight
	}
	// Reserve space for our own border + header + footer (2 + 1 + 1).
	return max(20, w-2), max(5, h-4)
}

func (m Model) headerView(width int) string {
	mode := "inline"
	if m.mode == ModeSideBySide {
		mode = "side-by-side"
	}
	side := ""
	if m.mode == ModeSideBySide {
		if m.side == SideLeft {
			side = " · side: ← old"
		} else {
			side = " · side: new →"
		}
	}
	left := fmt.Sprintf(" %s · #%d · %s", m.Repo, m.PRNumber, m.Title)
	right := fmt.Sprintf("mode: %s%s · pending: %d ", mode, side, len(m.pending))
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	style := lipgloss.NewStyle().Bold(true)
	return style.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) footerView(width int) string {
	hints := " j/k cursor · h/l side · V/⌃V select · c comment · R/A/X submit · tab mode · ? help · q close "
	if lipgloss.Width(hints) > width {
		hints = " c: comment · R: submit · ?: help · q: close "
	}
	return lipgloss.NewStyle().Faint(true).Render(hints)
}

func (m *Model) rebuild() {
	if m.Loading || m.err != nil {
		return
	}
	width, _ := m.size()
	m.doc = buildDoc(m.files, width, m.mode)
	m.codeRows = m.codeRows[:0]
	for i, r := range m.doc.rows {
		if r.kind == rowCode {
			m.codeRows = append(m.codeRows, i)
		}
	}
	if m.cursorRow < 0 && len(m.codeRows) > 0 {
		m.cursorRow = m.codeRows[0]
	}
	m.applyExistingMarkers()
	m.refreshViewport()
}

// refreshViewport rerenders the (already laid-out) doc with the current
// cursor / selection state. Cheap: no parsing or row layout happens.
func (m *Model) refreshViewport() {
	if m.Loading || m.err != nil {
		return
	}
	m.viewport.SetContent(m.doc.stringify(m.sel, m.cursorRow, m.side, m.comments))
}

// moveCursor advances the cursor by `delta` selectable code rows.
func (m *Model) moveCursor(delta int) {
	if len(m.codeRows) == 0 {
		return
	}
	idx := indexOf(m.codeRows, m.cursorRow)
	if idx < 0 {
		idx = 0
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.codeRows) {
		idx = len(m.codeRows) - 1
	}
	m.cursorRow = m.codeRows[idx]
	if m.sel.Mode != SelectNone {
		m.sel.CursorRow = m.cursorRow
	}
	m.refreshViewport()
}

// CursorRef returns the CodeRef under the cursor, if any.
func (m *Model) CursorRef() *CodeRef {
	if m.cursorRow < 0 || m.cursorRow >= len(m.doc.rows) {
		return nil
	}
	return m.doc.rows[m.cursorRow].ref
}

// SelectionRefs returns every CodeRef covered by the active selection (or
// just the cursor's ref if no selection is active). Empty when nothing is
// renderable. In side-by-side mode the refs are read from the active side;
// inline mode falls back to the row's primary ref.
func (m *Model) SelectionRefs() []CodeRef {
	if len(m.doc.rows) == 0 {
		return nil
	}
	lo, hi := m.cursorRow, m.cursorRow
	if m.sel.IsActive() {
		lo, hi = m.sel.Range()
	}
	side := m.side
	if m.sel.IsActive() {
		side = m.sel.Side
	}
	var out []CodeRef
	for i := lo; i <= hi && i < len(m.doc.rows); i++ {
		r := m.doc.rows[i]
		if r.kind != rowCode {
			continue
		}
		ref := pickSideRef(r, side)
		if ref != nil {
			out = append(out, *ref)
		}
	}
	return out
}

// pickSideRef returns the row's ref for the requested side, falling back to
// the other side / the primary ref if the requested half is empty (e.g. an
// add line has no LEFT counterpart).
func pickSideRef(r row, side Side) *CodeRef {
	if m := r.leftRef; side == SideLeft && m != nil {
		return m
	}
	if m := r.rightRef; side == SideRight && m != nil {
		return m
	}
	if r.leftRef != nil {
		return r.leftRef
	}
	if r.rightRef != nil {
		return r.rightRef
	}
	return r.ref
}

// StartSelection begins a selection in the given mode anchored at the cursor,
// pinned to the current active side.
func (m *Model) StartSelection(mode SelectMode) {
	if m.cursorRow < 0 {
		return
	}
	m.sel = Selection{
		Mode:      mode,
		AnchorRow: m.cursorRow,
		CursorRow: m.cursorRow,
		Side:      m.side,
	}
	m.refreshViewport()
}

// ClearSelection cancels any active selection.
func (m *Model) ClearSelection() {
	if m.sel.Mode == SelectNone {
		return
	}
	m.sel = Selection{}
	m.refreshViewport()
}

// SelectionMode reports the active selection mode.
func (m *Model) SelectionMode() SelectMode { return m.sel.Mode }

func indexOf(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
