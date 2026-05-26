package data

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"charm.land/log/v2"
	gh "github.com/cli/go-gh/v2/pkg/api"
)

// FetchPullRequestREST fetches a PR's comments + review threads via the
// REST API. This is used as a fallback when the GraphQL `resource(url:)`
// query fails (e.g. with "Resource not accessible by personal access
// token" on PRs whose commits the caller can't read). REST permission
// errors are scoped per-resource so we can still read comments even when
// commit-level access is denied.
//
// The function populates only the fields the Activity tab actually reads
// (Comments, ReviewThreads, Reviews) and leaves the rest zero — the
// caller should preserve any other fields it already has.
func FetchPullRequestREST(prUrl string) (EnrichedPullRequestData, error) {
	owner, repo, number, err := parsePRUrl(prUrl)
	if err != nil {
		return EnrichedPullRequestData{}, err
	}

	restClient, err := gh.DefaultRESTClient()
	if err != nil {
		return EnrichedPullRequestData{}, err
	}

	out := EnrichedPullRequestData{Url: prUrl, Number: number}

	if err := fillRESTIssueComments(restClient, owner, repo, number, &out); err != nil {
		log.Warn("REST issue comments fetch failed", "url", prUrl, "err", err)
	}
	if err := fillRESTReviewComments(restClient, owner, repo, number, &out); err != nil {
		log.Warn("REST review comments fetch failed", "url", prUrl, "err", err)
	}
	if err := fillRESTReviews(restClient, owner, repo, number, &out); err != nil {
		log.Warn("REST reviews fetch failed", "url", prUrl, "err", err)
	}

	return out, nil
}

// parsePRUrl extracts (owner, repo, number) from a github.com PR URL.
func parsePRUrl(prUrl string) (string, string, int, error) {
	u, err := url.Parse(prUrl)
	if err != nil {
		return "", "", 0, err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// expected: owner/repo/pull/<number>
	if len(parts) < 4 || parts[2] != "pull" {
		return "", "", 0, fmt.Errorf("unexpected PR URL: %s", prUrl)
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, err
	}
	return parts[0], parts[1], n, nil
}

type restIssueComment struct {
	User      struct{ Login string } `json:"user"`
	Body      string                 `json:"body"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func fillRESTIssueComments(client *gh.RESTClient, owner, repo string, n int, out *EnrichedPullRequestData) error {
	path := fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=100", owner, repo, n)
	var cs []restIssueComment
	if err := client.Get(path, &cs); err != nil {
		return err
	}
	for _, c := range cs {
		out.Comments.Nodes = append(out.Comments.Nodes, Comment{
			Body:      c.Body,
			UpdatedAt: c.UpdatedAt,
			Author: struct{ Login string }{
				Login: c.User.Login,
			},
		})
	}
	return nil
}

type restReviewComment struct {
	ID                int64                  `json:"id"`
	InReplyToID       int64                  `json:"in_reply_to_id"`
	User              struct{ Login string } `json:"user"`
	Body              string                 `json:"body"`
	UpdatedAt         time.Time              `json:"updated_at"`
	Path              string                 `json:"path"`
	Line              int                    `json:"line"`
	OriginalLine      int                    `json:"original_line"`
	StartLine         int                    `json:"start_line"`
	OriginalStartLine int                    `json:"original_start_line"`
	Position          *int                   `json:"position"` // nil when outdated
}

func fillRESTReviewComments(client *gh.RESTClient, owner, repo string, n int, out *EnrichedPullRequestData) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d/comments?per_page=100", owner, repo, n)
	var rcs []restReviewComment
	if err := client.Get(path, &rcs); err != nil {
		return err
	}
	// Group by review-thread root: each parent (in_reply_to_id == 0) starts
	// a thread, replies append to that thread.
	type threadEntry struct {
		Id           string
		IsOutdated   bool
		Path         string
		StartLine    int
		Line         int
		OriginalLine int
		Comments     []ReviewComment
	}
	threads := map[int64]*threadEntry{}
	for _, rc := range rcs {
		parent := rc.ID
		if rc.InReplyToID != 0 {
			parent = rc.InReplyToID
		}
		t, ok := threads[parent]
		if !ok {
			line := rc.Line
			if line == 0 {
				line = rc.OriginalLine
			}
			t = &threadEntry{
				Id:           fmt.Sprintf("rest-%d", parent),
				IsOutdated:   rc.Position == nil,
				Path:         rc.Path,
				StartLine:    rc.StartLine,
				Line:         line,
				OriginalLine: rc.OriginalLine,
			}
			threads[parent] = t
		}
		t.Comments = append(t.Comments, ReviewComment{
			Body:      rc.Body,
			UpdatedAt: rc.UpdatedAt,
			Author: struct{ Login string }{
				Login: rc.User.Login,
			},
			StartLine: rc.StartLine,
			Line:      rc.Line,
		})
	}
	// Drop into the same shape the GraphQL path produces so the Activity
	// tab can read both interchangeably.
	for _, t := range threads {
		node := struct {
			Id           string
			IsOutdated   bool
			IsResolved   bool
			IsCollapsed  bool
			OriginalLine int
			StartLine    int
			Line         int
			Path         string
			Comments     ReviewComments `graphql:"comments(first: 20)"`
		}{
			Id:           t.Id,
			IsOutdated:   t.IsOutdated,
			OriginalLine: t.OriginalLine,
			StartLine:    t.StartLine,
			Line:         t.Line,
			Path:         t.Path,
			Comments: ReviewComments{
				Nodes:      t.Comments,
				TotalCount: len(t.Comments),
			},
		}
		out.ReviewThreads.Nodes = append(out.ReviewThreads.Nodes, node)
	}
	return nil
}

type restReview struct {
	User      struct{ Login string } `json:"user"`
	Body      string                 `json:"body"`
	State     string                 `json:"state"`
	SubmittedAt time.Time            `json:"submitted_at"`
}

func fillRESTReviews(client *gh.RESTClient, owner, repo string, n int, out *EnrichedPullRequestData) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d/reviews?per_page=100", owner, repo, n)
	var rs []restReview
	if err := client.Get(path, &rs); err != nil {
		return err
	}
	// Decode JSON without surfacing the unmarshal call (the gh client
	// already did it). Convert into our internal Review shape.
	for _, r := range rs {
		out.Reviews.Nodes = append(out.Reviews.Nodes, Review{
			Body:      r.Body,
			State:     r.State,
			UpdatedAt: r.SubmittedAt,
			Author: struct{ Login string }{
				Login: r.User.Login,
			},
		})
	}
	return nil
}

// Quiet the unused-package lint when callers do not import json directly.
var _ = json.Unmarshal
