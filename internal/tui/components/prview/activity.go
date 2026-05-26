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
	UpdatedAt      time.Time
	RenderedString string
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
				UpdatedAt:      thread.Comments.Nodes[0].UpdatedAt,
				RenderedString: summary,
			})
		}
		for _, c := range thread.Comments.Nodes {
			comments = append(comments, comment{
				Author:    c.Author.Login,
				Body:      c.Body,
				UpdatedAt: c.UpdatedAt,
				Path:      &path,
				Line:      &line,
			})
		}
	}

	for _, c := range m.pr.Data.Enriched.Comments.Nodes {
		comments = append(comments, comment{
			Author:    c.Author.Login,
			Body:      c.Body,
			UpdatedAt: c.UpdatedAt,
		})
	}

	for _, comment := range comments {
		renderedComment, err := m.renderComment(comment, markdownRenderer)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			UpdatedAt:      comment.UpdatedAt,
			RenderedString: renderedComment,
		})
	}

	for _, review := range m.pr.Data.Primary.Reviews.Nodes {
		renderedReview, err := m.renderReview(review, markdownRenderer)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			UpdatedAt:      review.UpdatedAt,
			RenderedString: renderedReview,
		})
	}

	sort.Slice(activities, func(i, j int) bool {
		return activities[i].UpdatedAt.Before(activities[j].UpdatedAt)
	})

	body := ""
	if len(activities) == 0 {
		body = renderEmptyState()
	} else {
		var renderedActivities []string
		for _, activity := range activities {
			renderedActivities = append(renderedActivities, activity.RenderedString)
		}
		// Count folded (resolved) threads so we can hint at `T` to expand them.
		var folded int
		for _, t := range m.pr.Data.Enriched.ReviewThreads.Nodes {
			if t.IsResolved && !m.expandedThreads[t.Id] {
				folded++
			}
		}
		titleText := fmt.Sprintf("%s  %d comments", constants.CommentsIcon, len(activities))
		if folded > 0 {
			titleText += "  " + lipgloss.NewStyle().Faint(true).
				Render(fmt.Sprintf("· %d folded (press T to expand)", folded))
		}
		title := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(titleText)
		body = lipgloss.JoinVertical(lipgloss.Left, renderedActivities...)
		body = lipgloss.JoinVertical(lipgloss.Left, title, body)
	}

	return bodyStyle.Render(body)
}

func renderEmptyState() string {
	return lipgloss.NewStyle().Italic(true).Render("No comments...")
}

type comment struct {
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
