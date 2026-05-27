package sidebar

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

type Model struct {
	IsOpen     bool
	data       string
	viewport   viewport.Model
	ctx        *context.ProgramContext
	emptyState string
}

func NewModel() Model {
	vp := viewport.New(
		viewport.WithWidth(0),
		viewport.WithHeight(0),
	)

	return Model{
		IsOpen:     false,
		data:       "",
		viewport:   vp,
		ctx:        nil,
		emptyState: "Nothing selected...",
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Keys.PageDown):
			m.viewport.HalfPageDown()

		case key.Matches(msg, keys.Keys.PageUp):
			m.viewport.HalfPageUp()
		}
		// Vim-style scrolling keys for the PR sidebar. These are only
		// useful when the sidebar has focus (full-screen PR detail mode)
		// — in the inline layout the parent component owns these keys
		// for cursor movement in the list, so we tolerate them being
		// double-handled here (the viewport just scrolls a row that
		// most users won't notice).
		switch msg.String() {
		case "j", "down":
			m.viewport.ScrollDown(1)
		case "k", "up":
			m.viewport.ScrollUp(1)
		case "g", "home":
			m.viewport.GotoTop()
		case "G", "end":
			m.viewport.GotoBottom()
		case "ctrl+d":
			m.viewport.HalfPageDown()
		case "ctrl+u":
			m.viewport.HalfPageUp()
		}
	}

	return m, nil
}

func (m Model) View() string {
	if !m.IsOpen {
		return ""
	}

	if m.ctx.PreviewPosition == "bottom" {
		height := m.ctx.DynamicPreviewHeight
		width := m.ctx.DynamicPreviewWidth
		style := m.ctx.Styles.Sidebar.BottomRoot.
			Height(height).
			Width(width)

		if m.data == "" {
			return style.Align(lipgloss.Center).Render(
				lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
			)
		}

		return style.Render(lipgloss.JoinVertical(
			lipgloss.Top,
			m.viewport.View(),
			m.ctx.Styles.Sidebar.PagerStyle.
				Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))),
		))
	}

	// Right mode
	height := m.ctx.MainContentHeight
	style := m.ctx.Styles.Sidebar.Root.
		Height(height).
		Width(m.ctx.DynamicPreviewWidth)

	if m.data == "" {
		return style.Align(lipgloss.Center).Render(
			lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
		)
	}

	return style.Render(lipgloss.JoinVertical(
		lipgloss.Top,
		m.viewport.View(),
		m.ctx.Styles.Sidebar.PagerStyle.
			Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))),
	))
}

func (m *Model) SetContent(data string) {
	m.data = data
	m.viewport.SetContent(data)
}

func (m *Model) GetSidebarContentWidth() int {
	if m.ctx == nil || m.ctx.Config == nil {
		return 0
	}
	if m.ctx.PreviewPosition == "bottom" {
		return max(0, m.ctx.DynamicPreviewWidth)
	}
	return max(0, m.ctx.DynamicPreviewWidth-m.ctx.Styles.Sidebar.BorderWidth)
}

func (m *Model) ScrollToTop() {
	m.viewport.GotoTop()
}

func (m *Model) ScrollToBottom() {
	m.viewport.GotoBottom()
}

func (m *Model) YOffset() int {
	return m.viewport.YOffset()
}

// SetYOffset moves the viewport to start at the given line offset.
func (m *Model) SetYOffset(n int) {
	m.viewport.SetYOffset(n)
}

// EnsureVisible scrolls the viewport the minimum amount so that the line
// range [start, start+height) is fully visible. If the range is taller than
// the viewport we align its top. Already-visible ranges are left untouched
// so navigation feels stable.
func (m *Model) EnsureVisible(start, height int) {
	if height < 1 {
		height = 1
	}
	h := m.viewport.Height()
	if h <= 0 {
		return
	}
	top := m.viewport.YOffset()
	bottom := top + h
	end := start + height

	switch {
	case start < top:
		// Above the viewport — bring the start to the top.
		m.viewport.SetYOffset(start)
	case end > bottom:
		// Below the viewport — scroll just enough to reveal the end, but
		// never push the start off the top.
		want := end - h
		if want > start {
			want = start
		}
		m.viewport.SetYOffset(want)
	}
}

func (m *Model) ScrollToPercent(percent float64) {
	totalLines := m.viewport.TotalLineCount()
	targetLine := int(float64(totalLines) * percent)
	m.viewport.SetYOffset(targetLine)
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	if ctx == nil {
		return
	}
	m.ctx = ctx
	if m.ctx.PreviewPosition == "bottom" {
		m.viewport.SetHeight(m.ctx.DynamicPreviewHeight - m.ctx.Styles.Sidebar.PagerHeight)
	} else {
		m.viewport.SetHeight(m.ctx.MainContentHeight - m.ctx.Styles.Sidebar.PagerHeight)
	}
	m.viewport.SetWidth(m.GetSidebarContentWidth())
}
