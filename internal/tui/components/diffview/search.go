package diffview

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// searchHit is a single match in the file/hunk/code search.
type searchHit struct {
	kind     hitKind
	fileIdx  int    // -1 means "skip jump"
	hunkIdx  int    // -1 when file-level
	lineIdx  int    // -1 when file or hunk header level
	display  string // already-styled preview text
	rawLabel string // ANSI-free label used for screen-reader-ish fallback
}

type hitKind int

const (
	hitFile hitKind = iota
	hitHunk
	hitLine
)

// searchUI is the modal state for the in-diff search prompt.
type searchUI struct {
	active   bool
	input    textinput.Model
	hits     []searchHit
	selected int
}

func newSearchUI() searchUI {
	ti := textinput.New()
	ti.Placeholder = "Search files, hunks, code…"
	ti.Prompt = "/ "
	ti.SetWidth(40)
	return searchUI{input: ti}
}

func (s *searchUI) open(width int) {
	s.active = true
	s.input.SetWidth(min(80, max(30, width-10)))
	s.input.SetValue("")
	s.input.Focus()
	s.hits = nil
	s.selected = 0
}

func (s *searchUI) cancel() {
	s.active = false
	s.input.Blur()
	s.input.SetValue("")
	s.hits = nil
	s.selected = 0
}

func (s *searchUI) update(msg tea.Msg) tea.Cmd {
	if !s.active {
		return nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd
}

func (s *searchUI) clampSelection() {
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.hits) {
		s.selected = max(0, len(s.hits)-1)
	}
}

// runSearch scans files for substring matches against the current query.
// Match order: file path > hunk header > code line. Capped at 200 results.
func (s *searchUI) runSearch(files []File) {
	q := strings.TrimSpace(s.input.Value())
	s.hits = s.hits[:0]
	s.selected = 0
	if q == "" {
		return
	}
	const maxResults = 200
	lower := strings.ToLower(q)
	hitStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	pathStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	hunkStyle := lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("13"))

	push := func(h searchHit) bool {
		s.hits = append(s.hits, h)
		return len(s.hits) < maxResults
	}

	// Pass 1: file path matches.
	for fi, f := range files {
		path := f.Path()
		if !strings.Contains(strings.ToLower(path), lower) {
			continue
		}
		display := pathStyle.Render("▸ "+path) + "  " + hitStyle.Render("[file]")
		if !push(searchHit{kind: hitFile, fileIdx: fi, hunkIdx: -1, lineIdx: -1, display: display, rawLabel: path}) {
			return
		}
	}

	// Pass 2: hunk header matches.
	for fi, f := range files {
		for hi, h := range f.Hunks {
			if !strings.Contains(strings.ToLower(h.Header), lower) {
				continue
			}
			label := fmt.Sprintf("@@ -%d +%d @@ %s", h.OldStart, h.NewStart, h.Header)
			display := hunkStyle.Render(label) + "  " + lipgloss.NewStyle().Faint(true).Render(f.Path())
			if !push(searchHit{kind: hitHunk, fileIdx: fi, hunkIdx: hi, lineIdx: -1, display: display, rawLabel: label}) {
				return
			}
		}
	}

	// Pass 3: code line matches.
	for fi, f := range files {
		for hi, h := range f.Hunks {
			for li, ln := range h.Lines {
				if !strings.Contains(strings.ToLower(ln.Text), lower) {
					continue
				}
				marker := " "
				switch ln.Kind {
				case LineAdd:
					marker = "+"
				case LineDel:
					marker = "-"
				}
				lineNum := ln.NewNum
				if lineNum == 0 {
					lineNum = ln.OldNum
				}
				label := fmt.Sprintf("%s %4d  %s", marker, lineNum, strings.TrimSpace(ln.Text))
				display := highlightMatch(label, lower) + "  " +
					lipgloss.NewStyle().Faint(true).Render(f.Path())
				if !push(searchHit{kind: hitLine, fileIdx: fi, hunkIdx: hi, lineIdx: li, display: display, rawLabel: label}) {
					return
				}
			}
		}
	}
}

// highlightMatch wraps occurrences of `needle` (lowercase) inside `s` with a
// bold yellow style. Match is case-insensitive but the original casing of s
// is preserved.
func highlightMatch(s, needle string) string {
	if needle == "" {
		return s
	}
	lower := strings.ToLower(s)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	var b strings.Builder
	i := 0
	for i < len(s) {
		idx := strings.Index(lower[i:], needle)
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+idx])
		end := i + idx + len(needle)
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(style.Render(s[i+idx : end]))
		i = end
	}
	return b.String()
}

// view renders the search modal's box content. The caller positions it.
func (s *searchUI) view(maxWidth, maxResults int) string {
	if !s.active {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("11")).
		Background(lipgloss.Color("0")).
		Padding(0, 1)

	title := lipgloss.NewStyle().Bold(true).Render("Search")
	hints := lipgloss.NewStyle().Faint(true).
		Render("↑/↓ select  ·  enter jump  ·  esc cancel")

	var resultLines []string
	if len(s.hits) == 0 {
		if s.input.Value() == "" {
			resultLines = []string{lipgloss.NewStyle().Faint(true).Render("type to search…")}
		} else {
			resultLines = []string{lipgloss.NewStyle().Faint(true).Render("no matches")}
		}
	} else {
		shown := len(s.hits)
		if shown > maxResults {
			shown = maxResults
		}
		// Slide window so the selected item stays visible.
		start := 0
		end := shown
		if s.selected >= shown {
			start = s.selected - shown + 1
			end = s.selected + 1
		}
		if end > len(s.hits) {
			end = len(s.hits)
			start = max(0, end-shown)
		}
		sel := lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
		for i := start; i < end; i++ {
			line := s.hits[i].display
			if i == s.selected {
				line = sel.Render("› " + capDisplay(line, maxWidth-6))
			} else {
				line = "  " + capDisplay(line, maxWidth-6)
			}
			resultLines = append(resultLines, line)
		}
		if len(s.hits) > shown {
			resultLines = append(resultLines,
				lipgloss.NewStyle().Faint(true).Render(
					fmt.Sprintf("…+%d more", len(s.hits)-shown)))
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		s.input.View(),
		"",
		lipgloss.JoinVertical(lipgloss.Left, resultLines...),
		"",
		hints,
	)
	return box.Render(content)
}
