package prview

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// activitySearch is the modal state for searching the Activity tab.
type activitySearch struct {
	active   bool
	input    textinput.Model
	hits     []activitySearchHit
	selected int
}

// activitySearchHit identifies one match in the rendered Activity stream.
type activitySearchHit struct {
	AnchorIndex int    // index into Model.activityAnchors
	Author      string
	Kind        string
	Snippet     string
}

func newActivitySearch() activitySearch {
	ti := textinput.New()
	ti.Placeholder = "Search comments…"
	ti.Prompt = "/ "
	ti.SetWidth(40)
	return activitySearch{input: ti}
}

func (s *activitySearch) open(width int) {
	s.active = true
	s.input.SetWidth(min(80, max(30, width-10)))
	s.input.SetValue("")
	s.input.Focus()
	s.hits = nil
	s.selected = 0
}

func (s *activitySearch) cancel() {
	s.active = false
	s.input.Blur()
	s.input.SetValue("")
	s.hits = nil
	s.selected = 0
}

func (s *activitySearch) update(msg tea.Msg) tea.Cmd {
	if !s.active {
		return nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd
}

func (s *activitySearch) clampSelection() {
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.hits) {
		s.selected = max(0, len(s.hits)-1)
	}
}

// runSearch (re)computes the hit list against the current PR's activity
// payload. It's intentionally substring-based and case-insensitive — a
// good-enough fzf is overkill for typical PR comment volume.
func (m *Model) runActivitySearch() {
	m.search.hits = m.search.hits[:0]
	m.search.selected = 0
	q := strings.TrimSpace(m.search.input.Value())
	if q == "" {
		return
	}
	lower := strings.ToLower(q)
	if m.pr == nil || !m.pr.Data.IsEnriched {
		return
	}
	const maxResults = 200

	// Anchors are populated as a side effect of rendering Activity, so
	// build them up front to make sure we can jump to results.
	if len(m.activityAnchors) == 0 {
		_ = m.renderActivity()
	}
	// We need the underlying author / body for each anchor. Walk the same
	// raw sources renderActivity reads from and match indexes 1-to-1.
	type lookup struct {
		author, body, kind string
	}
	byId := map[string]lookup{}
	for _, thread := range m.pr.Data.Enriched.ReviewThreads.Nodes {
		for ci, c := range thread.Comments.Nodes {
			id := fmt.Sprintf("rc-%s-%d", thread.Id, ci)
			byId[id] = lookup{author: c.Author.Login, body: c.Body, kind: "comment"}
		}
	}
	for ci, c := range m.pr.Data.Enriched.Comments.Nodes {
		id := fmt.Sprintf("ic-%d", ci)
		byId[id] = lookup{author: c.Author.Login, body: c.Body, kind: "comment"}
	}
	for ri, r := range m.pr.Data.Primary.Reviews.Nodes {
		id := fmt.Sprintf("review-%d", ri)
		byId[id] = lookup{author: r.Author.Login, body: r.Body, kind: "review"}
	}

	for i, anc := range m.activityAnchors {
		entry, ok := byId[anc.Id]
		if !ok {
			continue
		}
		if !strings.Contains(strings.ToLower(entry.author), lower) &&
			!strings.Contains(strings.ToLower(entry.body), lower) {
			continue
		}
		// Build a short single-line snippet, prefer the first non-empty
		// body line, otherwise the author name.
		snip := ""
		for _, line := range strings.Split(entry.body, "\n") {
			s := strings.TrimSpace(line)
			if s == "" {
				continue
			}
			if len(s) > 80 {
				s = s[:79] + "…"
			}
			snip = s
			break
		}
		m.search.hits = append(m.search.hits, activitySearchHit{
			AnchorIndex: i,
			Author:      entry.author,
			Kind:        entry.kind,
			Snippet:     snip,
		})
		if len(m.search.hits) >= maxResults {
			break
		}
	}
}

// ActivitySearchActive reports whether the search modal is currently open.
func (m *Model) ActivitySearchActive() bool { return m.search.active }

// OpenActivitySearch opens the search modal. Width is the rendered PR
// view width so the modal sizes itself sensibly.
func (m *Model) OpenActivitySearch(width int) {
	if m.search.input.Placeholder == "" {
		m.search = newActivitySearch()
	}
	m.search.open(width)
}

// CloseActivitySearch dismisses the modal without picking a result.
func (m *Model) CloseActivitySearch() {
	m.search.cancel()
}

// ActivitySearchAcceptCurrent jumps the activity cursor to the highlighted
// hit (if any) and closes the modal. Returns true when a jump happened.
func (m *Model) ActivitySearchAcceptCurrent() bool {
	if !m.search.active || len(m.search.hits) == 0 {
		m.search.cancel()
		return false
	}
	hit := m.search.hits[m.search.selected]
	if hit.AnchorIndex >= 0 && hit.AnchorIndex < len(m.activityAnchors) {
		m.activityCursor = hit.AnchorIndex
	}
	m.search.cancel()
	return true
}

// ActivitySearchMove shifts the selection by `delta` and re-clamps.
func (m *Model) ActivitySearchMove(delta int) {
	if !m.search.active {
		return
	}
	m.search.selected += delta
	m.search.clampSelection()
}

// ActivitySearchKey forwards a key to the textinput, recomputing matches
// when the query changes.
func (m *Model) ActivitySearchKey(msg tea.Msg) tea.Cmd {
	if !m.search.active {
		return nil
	}
	prev := m.search.input.Value()
	cmd := m.search.update(msg)
	if m.search.input.Value() != prev {
		m.runActivitySearch()
	}
	return cmd
}

// ActivitySearchView returns the modal's rendered content. The parent
// places it as its own compositor layer above the PR detail view.
func (m *Model) ActivitySearchView(maxWidth int) string {
	if !m.search.active {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("11")).
		Background(lipgloss.Color("0")).
		Padding(0, 1)
	title := lipgloss.NewStyle().Bold(true).Render("Search comments")
	hint := lipgloss.NewStyle().Faint(true).
		Render("↑/↓ select  ·  enter jump  ·  esc cancel")

	var lines []string
	if len(m.search.hits) == 0 {
		if m.search.input.Value() == "" {
			lines = []string{lipgloss.NewStyle().Faint(true).Render("type to search…")}
		} else {
			lines = []string{lipgloss.NewStyle().Faint(true).Render("no matches")}
		}
	} else {
		const maxRows = 10
		start, end := 0, len(m.search.hits)
		if end > maxRows {
			if m.search.selected >= maxRows {
				start = m.search.selected - maxRows + 1
				end = m.search.selected + 1
			} else {
				end = maxRows
			}
		}
		sel := lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
		kindStyle := lipgloss.NewStyle().Faint(true)
		authorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
		for i := start; i < end; i++ {
			h := m.search.hits[i]
			row := fmt.Sprintf("%s  %s  %s",
				kindStyle.Render(fmt.Sprintf("[%s]", h.Kind)),
				authorStyle.Render(h.Author),
				h.Snippet)
			if len(row) > maxWidth-6 {
				row = row[:maxWidth-6] + "…"
			}
			if i == m.search.selected {
				row = sel.Render("› " + row)
			} else {
				row = "  " + row
			}
			lines = append(lines, row)
		}
		if len(m.search.hits) > maxRows {
			lines = append(lines, lipgloss.NewStyle().Faint(true).
				Render(fmt.Sprintf("…+%d more", len(m.search.hits)-maxRows)))
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		m.search.input.View(),
		"",
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		"",
		hint,
	)
	return box.Render(content)
}
