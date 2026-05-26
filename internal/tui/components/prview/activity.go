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
			summary := lipgloss.NewStyle().Faint(true).Render(
				fmt.Sprintf("▸ resolved [%d msgs] %s:%d  %s — %s",
					n, thread.Path, thread.Line, latest.Author.Login, snippet),
			)
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
			summary := lipgloss.NewStyle().Faint(true).Render(
				fmt.Sprintf("[%s] %s:%d", tag, thread.Path, thread.Line))
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
	authorAndTime := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.MainTextStyle.Render(comment.Author),
			" ",
			lipgloss.NewStyle().
				Foreground(m.ctx.Theme.FaintText).
				Render(utils.TimeElapsed(comment.UpdatedAt)),
		))

	var header string
	if comment.Path != nil && comment.Line != nil {
		filePath := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Width(width).Render(
			fmt.Sprintf(
				"%s#l%d",
				*comment.Path,
				*comment.Line,
			),
		)
		header = lipgloss.JoinVertical(lipgloss.Left, authorAndTime, filePath, "")
	} else {
		header = authorAndTime
	}

	body := lineCleanupRegex.ReplaceAllString(comment.Body, "")
	body, err := markdownRenderer.Render(body)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}

func (m *Model) renderReview(
	review data.Review,
	markdownRenderer glamour.TermRenderer,
) (string, error) {
	header := m.renderReviewHeader(review)
	body, err := markdownRenderer.Render(review.Body)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
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
