package diffview

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// PendingComment is a comment authored locally but not yet submitted to the
// GitHub PR review API. Phase 4 turns these into review-comment payloads.
type PendingComment struct {
	Path      string
	Side      string // "RIGHT" for additions/context, "LEFT" for deletions
	Line      int    // single-line target (line == endLine)
	StartLine int    // multi-line target (0 when single)
	Body      string
}

// commentEditor is the modal used to author a single PendingComment.
type commentEditor struct {
	active     bool
	textarea   textarea.Model
	targets    []CodeRef // refs the comment is attached to (already normalised)
	width      int
	height     int
}

func newCommentEditor() commentEditor {
	ta := textarea.New()
	ta.Placeholder = "Write a comment (markdown supported)…"
	ta.ShowLineNumbers = false
	ta.SetWidth(60)
	ta.SetHeight(6)
	return commentEditor{textarea: ta}
}

// open prepares the editor for the supplied selection.
func (c *commentEditor) open(targets []CodeRef, width int) {
	c.active = true
	c.targets = targets
	c.textarea.Reset()
	c.textarea.SetWidth(min(80, max(40, width-10)))
	c.textarea.SetHeight(6)
	c.textarea.Focus()
}

func (c *commentEditor) cancel() {
	c.active = false
	c.textarea.Reset()
	c.targets = nil
}

func (c *commentEditor) value() string {
	return strings.TrimSpace(c.textarea.Value())
}

func (c *commentEditor) view(screenW, screenH int) string {
	if !c.active {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("13")).
		Padding(0, 1)

	header := lipgloss.NewStyle().Bold(true).Render(c.headerLabel())
	hints := lipgloss.NewStyle().Faint(true).
		Render("ctrl+s: save  ·  esc: cancel")

	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		c.textarea.View(),
		hints,
	)
	rendered := box.Render(content)
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, rendered)
}

func (c *commentEditor) headerLabel() string {
	if len(c.targets) == 0 {
		return "Comment"
	}
	first := c.targets[0]
	last := c.targets[len(c.targets)-1]
	if first.Path == last.Path && first.LineIndex == last.LineIndex {
		line := first.New
		if line == 0 {
			line = first.Old
		}
		return shortPath(first.Path) + ":" + intToStr(line)
	}
	startLine, endLine := first.New, last.New
	if startLine == 0 {
		startLine = first.Old
	}
	if endLine == 0 {
		endLine = last.Old
	}
	return shortPath(first.Path) + ":" + intToStr(startLine) + "–" + intToStr(endLine)
}

func shortPath(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 && len(p)-i < 30 {
		return "…/" + p[i+1:]
	}
	return p
}

func intToStr(n int) string {
	if n == 0 {
		return "?"
	}
	// avoid fmt to keep this hot path allocation-free; small range
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// update routes input through the textarea while the editor is active.
func (c *commentEditor) update(msg tea.Msg) tea.Cmd {
	if !c.active {
		return nil
	}
	var cmd tea.Cmd
	c.textarea, cmd = c.textarea.Update(msg)
	return cmd
}

// buildPending collapses a (non-empty) sequence of CodeRefs into a single
// PendingComment ready for the GitHub review API. The GitHub schema expects
// the comment to be anchored on either the LEFT (deleted) or RIGHT (added /
// context) side of the diff, with optional `start_line` for multi-line.
func buildPending(targets []CodeRef, body string) PendingComment {
	if len(targets) == 0 {
		return PendingComment{}
	}
	first := targets[0]
	last := targets[len(targets)-1]

	pc := PendingComment{
		Path: first.Path,
		Body: body,
	}
	// Determine side from the first ref. LineDel rows only have an Old line
	// number; everything else (Add / Context) anchors on the new side.
	if first.Kind == LineDel {
		pc.Side = "LEFT"
		pc.Line = lastNonZero(last.Old, first.Old)
		if first.Old != last.Old {
			pc.StartLine = first.Old
		}
	} else {
		pc.Side = "RIGHT"
		pc.Line = lastNonZero(last.New, first.New)
		if first.New != 0 && first.New != last.New {
			pc.StartLine = first.New
		}
	}
	return pc
}

func lastNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
