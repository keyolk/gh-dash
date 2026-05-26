package diffview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ExistingComment is a comment fetched from the GitHub review API. Only the
// fields the diff viewer actually renders are kept here.
type ExistingComment struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Side      string `json:"side"`        // LEFT / RIGHT
	Line      int    `json:"line"`        // single-line / multi-line end
	StartLine int    `json:"start_line"`  // 0 for single-line
	Body      string `json:"body"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// CommentsFetched is dispatched when the existing-comments fetch completes.
type CommentsFetched struct {
	PRNumber int
	Repo     string
	Comments []ExistingComment
	Err      error
}

// ReviewSubmitted is dispatched after a review is posted.
type ReviewSubmitted struct {
	PRNumber int
	Repo     string
	Event    string
	Err      error
}

// fetchExistingComments asks the GitHub API for every review comment on a PR.
// It returns a tea.Cmd so callers can compose it into their command stream.
func fetchExistingComments(prNumber int, repo string) tea.Cmd {
	return func() tea.Msg {
		path := fmt.Sprintf("/repos/%s/pulls/%d/comments?per_page=100", repo, prNumber)
		out, err := exec.Command("gh", "api", path).Output()
		if err != nil {
			return CommentsFetched{PRNumber: prNumber, Repo: repo, Err: err}
		}
		var cs []ExistingComment
		if jerr := json.Unmarshal(out, &cs); jerr != nil {
			return CommentsFetched{PRNumber: prNumber, Repo: repo, Err: jerr}
		}
		return CommentsFetched{PRNumber: prNumber, Repo: repo, Comments: cs}
	}
}

// submitReview posts the supplied pending comments as a single PR review.
// `event` must be one of "COMMENT", "APPROVE", "REQUEST_CHANGES".
func submitReview(prNumber int, repo, event, body string, pending []PendingComment) tea.Cmd {
	return func() tea.Msg {
		type apiComment struct {
			Path      string `json:"path"`
			Side      string `json:"side,omitempty"`
			StartSide string `json:"start_side,omitempty"`
			Line      int    `json:"line"`
			StartLine int    `json:"start_line,omitempty"`
			Body      string `json:"body"`
		}
		payload := struct {
			Event    string       `json:"event"`
			Body     string       `json:"body,omitempty"`
			Comments []apiComment `json:"comments,omitempty"`
		}{Event: event, Body: body}
		for _, p := range pending {
			ac := apiComment{
				Path: p.Path, Side: p.Side,
				Line: p.Line, StartLine: p.StartLine,
				Body: p.Body,
			}
			// For multi-line comments GitHub requires both side and start_side.
			if p.StartLine != 0 {
				ac.StartSide = p.Side
			}
			payload.Comments = append(payload.Comments, ac)
		}
		buf, err := json.Marshal(payload)
		if err != nil {
			return ReviewSubmitted{PRNumber: prNumber, Repo: repo, Event: event, Err: err}
		}
		path := fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, prNumber)
		cmd := exec.Command("gh", "api", "--method", "POST", path, "--input", "-")
		cmd.Stdin = bytes.NewReader(buf)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if _, perr := cmd.Output(); perr != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = perr.Error()
			}
			return ReviewSubmitted{
				PRNumber: prNumber, Repo: repo, Event: event,
				Err: fmt.Errorf("gh api: %s", msg),
			}
		}
		return ReviewSubmitted{PRNumber: prNumber, Repo: repo, Event: event}
	}
}
