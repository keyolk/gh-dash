package prview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type RenderedActivity struct {
	Id             string // stable identifier for fold / cursor tracking
	Kind           string // "comment" | "review" | "thread-banner" | "resolved-summary"
	UpdatedAt      time.Time
	RenderedString string
}

// ActivityAnchor pins a rendered activity to the line index inside the PR
// view's content stream so the parent can scroll directly to it.
type ActivityAnchor struct {
	Id   string
	Kind string
	Line int // 0-based row index of the activity's first line
}

func (m *Model) renderActivity() string {
	width := m.getIndentedContentWidth()
	markdownRenderer := markdown.GetMarkdownRenderer(width)
	bodyStyle := lipgloss.NewStyle()

	var activities []RenderedActivity
	var comments []comment

	// While the enriched payload is in-flight we can still show the
	// reviews we already have on the primary PR record, plus a small
	// "loading…" hint at the top. This avoids the user staring at a
	// blank "Loading..." screen waiting for a slow GraphQL response.
	if !m.pr.Data.IsEnriched {
		var partial []string
		hint := lipgloss.NewStyle().Italic(true).
			Foreground(m.ctx.Theme.FaintText).
			Render("Loading comments…")
		if m.enrichErr != nil {
			hint = lipgloss.NewStyle().Bold(true).
				Foreground(m.ctx.Theme.ErrorText).
				Render(fmt.Sprintf("Failed to load comments: %v   (press U to retry)", m.enrichErr))
		}
		partial = append(partial, hint)
		for _, review := range m.pr.Data.Primary.Reviews.Nodes {
			renderedReview, err := m.renderReview(review, markdownRenderer)
			if err != nil {
				continue
			}
			partial = append(partial, renderedReview)
		}
		if len(partial) == 1 {
			return bodyStyle.Render(hint)
		}
		return bodyStyle.Render(lipgloss.JoinVertical(lipgloss.Left, partial...))
	}

	for _, thread := range m.pr.Data.Enriched.ReviewThreads.Nodes {
		// Only auto-fold *resolved* threads — outdated / collapsed threads
		// often still contain context the user wants to read at a glance,
		// and force-pushes mark a lot of live discussion as outdated. The
		// user can still press T to fully unfold resolved threads.
		folded := thread.IsResolved
		if folded && !m.expandedThreads[thread.Id] {
			n := len(thread.Comments.Nodes)
			if n == 0 {
				continue
			}
			latest := thread.Comments.Nodes[n-1]
			snippet := strings.SplitN(strings.TrimSpace(latest.Body), "\n", 2)[0]
			if len(snippet) > 80 {
				snippet = snippet[:79] + "…"
			}
			label := lipgloss.NewStyle().
				Foreground(lipgloss.Color("11")).Bold(true).
				Render("▶ resolved")
			meta := lipgloss.NewStyle().Faint(true).Render(
				fmt.Sprintf("  [%d msgs]  %s:%d  ", n, thread.Path, thread.Line))
			authorTxt := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).
				Render(latest.Author.Login + " — ")
			line := label + meta + authorTxt + snippet
			summary := lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderLeft(true).
				BorderForeground(lipgloss.Color("11")).
				PaddingLeft(1).
				Render(line)
			activities = append(activities, RenderedActivity{
				Id:             "thread-" + thread.Id,
				Kind:           "resolved-summary",
				UpdatedAt:      latest.UpdatedAt,
				RenderedString: summary,
			})
			continue
		}
		path := thread.Path
		line := thread.Line
		// Tag outdated / collapsed threads so the user knows the context,
		// but still show the full conversation.
		var tag string
		switch {
		case thread.IsOutdated:
			tag = "outdated"
		case thread.IsCollapsed:
			tag = "collapsed"
		}
		if tag != "" {
			banner := lipgloss.NewStyle().
				Foreground(lipgloss.Color("13")).Bold(true).
				Render("◉ " + tag)
			loc := lipgloss.NewStyle().Faint(true).Render(
				fmt.Sprintf("  %s:%d", thread.Path, thread.Line))
			summary := lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderLeft(true).
				BorderForeground(lipgloss.Color("13")).
				PaddingLeft(1).
				Render(banner + loc)
			activities = append(activities, RenderedActivity{
				Id:             "banner-" + thread.Id,
				Kind:           "thread-banner",
				UpdatedAt:      thread.Comments.Nodes[0].UpdatedAt,
				RenderedString: summary,
			})
		}
		for ci, c := range thread.Comments.Nodes {
			comments = append(comments, comment{
				Id:        fmt.Sprintf("rc-%s-%d", thread.Id, ci),
				Author:    c.Author.Login,
				Body:      c.Body,
				UpdatedAt: c.UpdatedAt,
				Path:      &path,
				Line:      &line,
			})
		}
	}

	for ci, c := range m.pr.Data.Enriched.Comments.Nodes {
		comments = append(comments, comment{
			Id:        fmt.Sprintf("ic-%d", ci),
			Author:    c.Author.Login,
			Body:      c.Body,
			UpdatedAt: c.UpdatedAt,
		})
	}

	for _, c := range comments {
		renderedComment, err := m.renderComment(c, markdownRenderer)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			Id:             c.Id,
			Kind:           "comment",
			UpdatedAt:      c.UpdatedAt,
			RenderedString: renderedComment,
		})
	}

	for ri, review := range m.pr.Data.Primary.Reviews.Nodes {
		renderedReview, err := m.renderReview(review, markdownRenderer)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			Id:             fmt.Sprintf("review-%d", ri),
			Kind:           "review",
			UpdatedAt:      review.UpdatedAt,
			RenderedString: renderedReview,
		})
	}

	sort.Slice(activities, func(i, j int) bool {
		return activities[i].UpdatedAt.Before(activities[j].UpdatedAt)
	})

	// Apply user-side folds (press `f` over a comment to collapse it) by
	// swapping the rendered body for a one-liner.
	for i, act := range activities {
		if m.foldedActivities[act.Id] {
			activities[i].RenderedString = m.renderFoldedActivity(act)
		}
	}

	// Re-anchor every activity so n / N can scroll directly to them and
	// remember the previously-selected cursor by id.
	prevCursorId := ""
	if m.activityCursor >= 0 && m.activityCursor < len(m.activityAnchors) {
		prevCursorId = m.activityAnchors[m.activityCursor].Id
	}
	m.activityAnchors = m.activityAnchors[:0]

	body := ""
	if len(activities) == 0 {
		body = renderEmptyState()
	} else {
		var renderedActivities []string
		// Count folded (resolved) threads so we can hint at `T` to expand them.
		var folded int
		for _, t := range m.pr.Data.Enriched.ReviewThreads.Nodes {
			if t.IsResolved && !m.expandedThreads[t.Id] {
				folded++
			}
		}
		var userFolded int
		for _, act := range activities {
			if m.foldedActivities[act.Id] {
				userFolded++
			}
		}
		titleText := fmt.Sprintf("%s  %d comments", constants.CommentsIcon, len(activities))
		if folded > 0 {
			titleText += "  " + lipgloss.NewStyle().Faint(true).
				Render(fmt.Sprintf("· %d resolved folded (T)", folded))
		}
		if userFolded > 0 {
			titleText += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("11")).
				Render(fmt.Sprintf("· %d minimised (f)", userFolded))
		}
		titleText += "  " + lipgloss.NewStyle().Faint(true).
			Render("· n/N next/prev")
		title := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(titleText)
		renderedActivities = append(renderedActivities, title)

		// Track line offsets so cursor / next-prev navigation works.
		// title occupies its own row plus a margin.
		currentLine := strings.Count(title, "\n") + 1 + 1 // +1 for MarginBottom

		for i, act := range activities {
			rendered := act.RenderedString
			if i == m.activityCursor {
				rendered = m.highlightCursor(rendered)
			}
			m.activityAnchors = append(m.activityAnchors, ActivityAnchor{
				Id: act.Id, Kind: act.Kind, Line: currentLine,
			})
			renderedActivities = append(renderedActivities, rendered)
			currentLine += strings.Count(rendered, "\n") + 1
		}
		body = lipgloss.JoinVertical(lipgloss.Left, renderedActivities...)
	}

	// Restore the cursor index after a re-layout (e.g. a fold changes a
	// row's id ordering would otherwise leave the cursor on the wrong
	// activity).
	if prevCursorId != "" {
		for i, a := range m.activityAnchors {
			if a.Id == prevCursorId {
				m.activityCursor = i
				break
			}
		}
	}

	return bodyStyle.Render(body)
}

func renderEmptyState() string {
	return lipgloss.NewStyle().Italic(true).Render("No comments...")
}

// renderFoldedActivity collapses a comment / review into a single line the
// user can still scan. Only the first non-empty body line is shown.
func (m *Model) renderFoldedActivity(act RenderedActivity) string {
	// Try to extract a body preview from the rendered content's first
	// few lines. We strip ANSI by taking only printable runes after we
	// search for the title fragment.
	preview := strings.SplitN(strings.TrimSpace(act.RenderedString), "\n", 6)
	var hint string
	for _, p := range preview {
		s := strings.TrimSpace(stripANSIQuick(p))
		if s == "" || strings.HasPrefix(s, "╭") || strings.HasPrefix(s, "│") {
			continue
		}
		if len(s) > 80 {
			s = s[:79] + "…"
		}
		hint = s
		break
	}
	kindBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).Bold(true).Render("▶ minimised")
	tail := lipgloss.NewStyle().Faint(true).Render(
		fmt.Sprintf("  [%s · %s]  %s",
			act.Kind, utils.TimeElapsed(act.UpdatedAt), hint))
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("11")).
		PaddingLeft(1).
		Render(kindBadge + tail)
}

// highlightCursor wraps a rendered activity in a left bar that marks it as
// the current selection from n / N navigation.
func (m *Model) highlightCursor(s string) string {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("12")).
		PaddingLeft(1).
		Render(s)
}

// stripANSIQuick removes ANSI escape sequences. We don't import a heavier
// dep just for this — comments aren't long enough to need it.
func stripANSIQuick(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

type comment struct {
	Id        string
	Author    string
	UpdatedAt time.Time
	Body      string
	Path      *string
	Line      *int
}

func (m *Model) renderComment(
	comment comment,
	markdownRenderer glamour.TermRenderer,
) (string, error) {
	width := m.getIndentedContentWidth()
	innerWidth := width - 2 // border eats 2 cells
	if innerWidth < 10 {
		innerWidth = 10
	}

	authorLine := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.ctx.Styles.Common.MainTextStyle.Render(comment.Author),
		"  ",
		lipgloss.NewStyle().
			Foreground(m.ctx.Theme.FaintText).
			Render(utils.TimeElapsed(comment.UpdatedAt)),
	)

	var pathLine string
	if comment.Path != nil && comment.Line != nil {
		pathLine = lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).
			Render(fmt.Sprintf("%s#L%d", *comment.Path, *comment.Line))
	}

	body := lineCleanupRegex.ReplaceAllString(comment.Body, "")
	rendered, err := markdownRenderer.Render(body)

	parts := []string{authorLine}
	if pathLine != "" {
		parts = append(parts, pathLine)
	}
	parts = append(parts, rendered)
	card := lipgloss.JoinVertical(lipgloss.Left, parts...)

	boxed := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Padding(0, 1).
		Width(innerWidth).
		Render(card)

	// One blank line after each comment so successive cards don't sit
	// glued together.
	return boxed + "\n", err
}

func (m *Model) renderReview(
	review data.Review,
	markdownRenderer glamour.TermRenderer,
) (string, error) {
	width := m.getIndentedContentWidth()
	innerWidth := width - 2
	if innerWidth < 10 {
		innerWidth = 10
	}
	header := m.renderReviewHeader(review)
	body, err := markdownRenderer.Render(review.Body)
	card := lipgloss.JoinVertical(lipgloss.Left, header, body)

	// Pick a border colour that reflects the review state so the user can
	// scan approvals / change requests at a glance.
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(innerWidth)
	switch review.State {
	case "APPROVED":
		box = box.BorderForeground(lipgloss.Color("10"))
	case "CHANGES_REQUESTED":
		box = box.BorderForeground(lipgloss.Color("9"))
	default:
		box = box.BorderForeground(m.ctx.Theme.FaintBorder)
	}
	return box.Render(card) + "\n", err
}

func (m *Model) renderReviewHeader(review data.Review) string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderReviewDecision(review.State),
		" ",
		m.ctx.Styles.Common.MainTextStyle.Render(review.Author.Login),
		" ",
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			"reviewed "+utils.TimeElapsed(review.UpdatedAt)),
	)
}

func (m *Model) renderReviewDecision(decision string) string {
	switch decision {
	case "PENDING":
		return m.ctx.Styles.Common.WaitingGlyph
	case "COMMENTED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render("󰈈")
	case "APPROVED":
		return m.ctx.Styles.Common.SuccessGlyph
	case "CHANGES_REQUESTED":
		return m.ctx.Styles.Common.FailureGlyph
	}

	return ""
}
