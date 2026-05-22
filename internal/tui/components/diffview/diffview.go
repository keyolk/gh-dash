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

	pending []PendingComment
	editor  commentEditor
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
	if m.comments == nil {
		m.comments = map[CodeRef]bool{}
	}
	m.viewport.SetContent("")
	m.viewport.GotoTop()

	return func() tea.Msg {
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
	}
	m.rebuild()
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

// EditorView returns the editor overlay (or "" when inactive). The top-level
// UI composes this as its own layer so it can sit above the diff frame.
func (m Model) EditorView() string {
	if !m.editor.active {
		return ""
	}
	w, h := m.size()
	return m.editor.view(w, h)
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
	left := fmt.Sprintf(" %s · #%d · %s", m.Repo, m.PRNumber, m.Title)
	right := fmt.Sprintf("mode: %s ", mode)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	style := lipgloss.NewStyle().Bold(true)
	return style.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) footerView(width int) string {
	hints := " j/k scroll · ctrl-d/u page · g/G top/bot · tab toggle mode · q/esc close "
	if lipgloss.Width(hints) > width {
		hints = " tab: mode · q: close "
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
	m.refreshViewport()
}

// refreshViewport rerenders the (already laid-out) doc with the current
// cursor / selection state. Cheap: no parsing or row layout happens.
func (m *Model) refreshViewport() {
	if m.Loading || m.err != nil {
		return
	}
	m.viewport.SetContent(m.doc.stringify(m.sel, m.cursorRow, m.comments))
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
// renderable.
func (m *Model) SelectionRefs() []CodeRef {
	if len(m.doc.rows) == 0 {
		return nil
	}
	lo, hi := m.cursorRow, m.cursorRow
	if m.sel.IsActive() {
		lo, hi, _, _ = m.sel.Range()
	}
	var out []CodeRef
	for i := lo; i <= hi && i < len(m.doc.rows); i++ {
		r := m.doc.rows[i]
		if r.kind == rowCode && r.ref != nil {
			out = append(out, *r.ref)
		}
	}
	return out
}

// StartSelection begins a selection in the given mode anchored at the cursor.
func (m *Model) StartSelection(mode SelectMode) {
	if m.cursorRow < 0 {
		return
	}
	m.sel = Selection{
		Mode:      mode,
		AnchorRow: m.cursorRow,
		CursorRow: m.cursorRow,
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
