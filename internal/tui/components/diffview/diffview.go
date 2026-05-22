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
	// rendered is the cached rendered diff text for the current mode/size.
	rendered string
}

// NewModel constructs an empty diff viewer.
func NewModel(ctx *context.ProgramContext) Model {
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	return Model{
		ctx:      ctx,
		viewport: vp,
		mode:     ModeSideBySide,
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
	m.rendered = ""
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
	m.rendered = ""
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
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
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
	switch m.mode {
	case ModeSideBySide:
		m.rendered = renderSideBySide(m.files, width)
	default:
		m.rendered = renderInline(m.files, width)
	}
	m.viewport.SetContent(m.rendered)
}
