package ghpr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A review ade builds is GitHub's own pending review. REST cannot add a
// comment to one that exists, so the lifecycle is GraphQL: see the design doc.

// The verdicts a pending review may be finished with. DISMISS is not one:
// it acts on somebody else's review.
const (
	EventApprove        = "APPROVE"
	EventRequestChanges = "REQUEST_CHANGES"
	EventComment        = "COMMENT"
)

// Draft is the viewer's pending review on a pull request, if there is one.
type Draft struct {
	ID       string
	Comments int
}

// PullRequestID is the node id of a pull request, which the mutations take
// instead of its number.
func PullRequestID(ctx context.Context, dir, owner, repo string, number int) (string, error) {
	var got struct {
		Data struct {
			Repository struct {
				PullRequest struct{ ID string }
			}
		}
	}
	if err := graphql(ctx, dir, queryPullRequestID, map[string]any{
		"owner": owner, "repo": repo, "number": number,
	}, &got); err != nil {
		return "", err
	}
	if got.Data.Repository.PullRequest.ID == "" {
		return "", fmt.Errorf("no pull request #%d in %s/%s", number, owner, repo)
	}
	return got.Data.Repository.PullRequest.ID, nil
}

// PendingReview is the draft the viewer has open, or a zero Draft. Read before
// every add: it may have been submitted or deleted from the GitHub UI since.
func PendingReview(ctx context.Context, dir, owner, repo string, number int) (Draft, error) {
	var got struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					Reviews struct {
						Nodes []struct {
							ID       string
							Comments struct{ TotalCount int }
						}
					}
				}
			}
		}
	}
	if err := graphql(ctx, dir, queryPendingReview, map[string]any{
		"owner": owner, "repo": repo, "number": number,
	}, &got); err != nil {
		return Draft{}, err
	}
	for _, node := range got.Data.Repository.PullRequest.Reviews.Nodes {
		if node.ID != "" {
			return Draft{ID: node.ID, Comments: node.Comments.TotalCount}, nil
		}
	}
	return Draft{}, nil
}

// StartDraftReview opens a pending review with one comment on it. No event, so
// GitHub leaves it pending and nobody but you can see it.
func StartDraftReview(ctx context.Context, dir, prID string, c InlineComment) (review, comment string, err error) {
	if err := c.ok(); err != nil {
		return "", "", err
	}
	var got struct {
		Data struct {
			AddPullRequestReview struct {
				PullRequestReview struct {
					ID       string
					Comments struct{ Nodes []struct{ ID string } }
				}
			}
		}
	}
	if err := graphql(ctx, dir, mutationStartDraft, map[string]any{
		"pr": prID, "path": c.Path, "line": c.Line, "side": c.graphSide(), "body": c.Body,
	}, &got); err != nil {
		return "", "", err
	}
	made := got.Data.AddPullRequestReview.PullRequestReview
	if made.ID == "" {
		return "", "", errors.New("GitHub did not say which review it started")
	}
	if len(made.Comments.Nodes) == 0 {
		return made.ID, "", notInDiff(c)
	}
	return made.ID, made.Comments.Nodes[0].ID, nil
}

// AddToDraftReview puts one more comment on the pending review and answers
// with the comment's id, which is what taking it back out needs.
func AddToDraftReview(ctx context.Context, dir, reviewID string, c InlineComment) (string, error) {
	if err := c.ok(); err != nil {
		return "", err
	}
	var got struct {
		Data struct {
			AddPullRequestReviewThread struct {
				Thread struct {
					Comments struct{ Nodes []struct{ ID string } }
				}
			}
		}
	}
	if err := graphql(ctx, dir, mutationAddThread, map[string]any{
		"review": reviewID, "path": c.Path, "line": c.Line, "side": c.graphSide(), "body": c.Body,
	}, &got); err != nil {
		return "", err
	}
	nodes := got.Data.AddPullRequestReviewThread.Thread.Comments.Nodes
	if len(nodes) == 0 {
		return "", notInDiff(c)
	}
	return nodes[0].ID, nil
}

// notInDiff is what an empty answer means. GitHub takes the mutation, answers
// 200 with no thread and no error, and creates nothing. Measured: see the doc.
func notInDiff(c InlineComment) error {
	return fmt.Errorf(
		"GitHub took no comment on %s:%d: a review comment can only go on a line the pull request's diff touches",
		c.Path, c.Line)
}

// RemoveFromDraftReview takes one comment back out of the pending review.
func RemoveFromDraftReview(ctx context.Context, dir, commentID string) error {
	if commentID == "" {
		return errors.New("no comment to take out")
	}
	return graphql(ctx, dir, mutationDeleteComment, map[string]any{"comment": commentID}, nil)
}

// SubmitDraftReview finishes the pending review, which is the moment the
// author can see any of it.
func SubmitDraftReview(ctx context.Context, dir, reviewID, event, body string) error {
	switch event {
	case EventApprove, EventRequestChanges, EventComment:
	default:
		return fmt.Errorf("unknown review event %q", event)
	}
	if reviewID == "" {
		return errors.New("no draft review to finish")
	}
	var got struct{}
	return graphql(ctx, dir, mutationSubmit, map[string]any{
		"review": reviewID, "event": event, "body": body,
	}, &got)
}

func (c InlineComment) ok() error {
	if c.Path == "" || c.Line <= 0 || strings.TrimSpace(c.Body) == "" {
		return errors.New("a comment needs a file, a line and text")
	}
	return nil
}

// graphSide is the DiffSide enum, which is spelled as PostReview's side is.
func (c InlineComment) graphSide() string {
	if strings.EqualFold(c.Side, "LEFT") || strings.EqualFold(c.Side, SideLeft) {
		return "LEFT"
	}
	return "RIGHT"
}

// The documents, as constants: nothing is interpolated into them, which is
// what lets graphql() check what it is about to run by name.
const (
	queryPullRequestID = `query($owner:String!,$repo:String!,$number:Int!){
  repository(owner:$owner,name:$repo){ pullRequest(number:$number){ id } }
}`

	queryPendingReview = `query($owner:String!,$repo:String!,$number:Int!){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      reviews(states:[PENDING],first:1,author:null){ nodes{ id comments{ totalCount } } }
    }
  }
}`

	mutationStartDraft = `mutation($pr:ID!,$path:String!,$line:Int!,$side:DiffSide!,$body:String!){
  addPullRequestReview(input:{
    pullRequestId:$pr,
    threads:[{path:$path,line:$line,side:$side,body:$body}]
  }){ pullRequestReview{ id comments(first:1){ nodes{ id } } } }
}`

	mutationAddThread = `mutation($review:ID!,$path:String!,$line:Int!,$side:DiffSide!,$body:String!){
  addPullRequestReviewThread(input:{
    pullRequestReviewId:$review,path:$path,line:$line,side:$side,body:$body
  }){ thread{ comments(first:1){ nodes{ id } } } }
}`

	mutationDeleteComment = `mutation($comment:ID!){
  deletePullRequestReviewComment(input:{id:$comment}){ clientMutationId }
}`

	mutationSubmit = `mutation($review:ID!,$event:PullRequestReviewEvent!,$body:String){
  submitPullRequestReview(input:{
    pullRequestReviewId:$review,event:$event,body:$body
  }){ pullRequestReview{ id state } }
}`
)

// mutationsAllowed are the only three mutations this package may run. Each is
// sent from a button that showed you the words first; anything else is refused.
var mutationsAllowed = map[string]bool{
	"addPullRequestReview":           true,
	"addPullRequestReviewThread":     true,
	"submitPullRequestReview":        true,
	"deletePullRequestReviewComment": true,
}

var mutationNamed = regexp.MustCompile(`\b([a-zA-Z]+)\s*\(input:`)

// allowedDocument reports whether a GraphQL document is one this package may
// run: a query, or a mutation calling only allowlisted fields.
func allowedDocument(query string) error {
	trimmed := strings.TrimSpace(query)
	if !strings.HasPrefix(trimmed, "mutation") {
		if strings.Contains(strings.ToLower(trimmed), "mutation") {
			return errors.New("ghpr will not run that GraphQL document")
		}
		return nil
	}
	found := mutationNamed.FindAllStringSubmatch(trimmed, -1)
	if len(found) == 0 {
		return errors.New("ghpr will not run a GraphQL mutation it cannot name")
	}
	for _, one := range found {
		if !mutationsAllowed[one[1]] {
			return fmt.Errorf("ghpr will not run the GraphQL mutation %q", one[1])
		}
	}
	return nil
}

// graphql runs one of this package's own documents as a single JSON payload
// on stdin. Nothing is interpolated into the query.
func graphql(ctx context.Context, dir, query string, vars map[string]any, into any) error {
	if err := allowedDocument(query); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("encode the GraphQL request: %w", err)
	}
	out, err := exec1(ctx, dir, []string{"api", "graphql", "--input", "-"}, payload)
	if err != nil {
		return err
	}
	// GitHub answers 200 with an errors array, so a failed mutation looks
	// like success to gh.
	var said struct {
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &said); err == nil && len(said.Errors) > 0 {
		return errors.New(said.Errors[0].Message)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal([]byte(out), into); err != nil {
		return fmt.Errorf("read GitHub's answer: %w", err)
	}
	return nil
}
