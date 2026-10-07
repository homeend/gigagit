package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// The mutations. Each operation name is what the fake gh records and what
// the runner's span is named after.
const (
	mStartReview = `mutation StartReview($pr:ID!,$commit:GitObjectID){addPullRequestReview(input:{pullRequestId:$pr,commitOID:$commit}){pullRequestReview{id}}}`
	mAddThread   = `mutation AddThread($review:ID!,$path:String!,$body:String!,$line:Int,$startLine:Int,$side:DiffSide,$startSide:DiffSide,$subject:PullRequestReviewThreadSubjectType){addPullRequestReviewThread(input:{pullRequestReviewId:$review,path:$path,body:$body,line:$line,startLine:$startLine,side:$side,startSide:$startSide,subjectType:$subject}){thread{id comments(first:1){nodes{id url}}}}}`
	mReply       = `mutation Reply($thread:ID!,$review:ID,$body:String!){addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$thread,pullRequestReviewId:$review,body:$body}){comment{id url}}}`
	mSubmit      = `mutation SubmitReview($review:ID!,$event:PullRequestReviewEvent!,$body:String){submitPullRequestReview(input:{pullRequestReviewId:$review,event:$event,body:$body}){pullRequestReview{id state}}}`
	mDelete      = `mutation DeleteReview($review:ID!){deletePullRequestReview(input:{pullRequestReviewId:$review}){pullRequestReview{id}}}`
	mResolve     = `mutation Resolve($thread:ID!){resolveReviewThread(input:{threadId:$thread}){thread{id isResolved}}}`
	mUnresolve   = `mutation Unresolve($thread:ID!){unresolveReviewThread(input:{threadId:$thread}){thread{id isResolved}}}`
)

var _ Writer = (*GH)(nil)

// mutate posts one mutation: the query and variables go in a temp file
// (gg's runner has no stdin), out receives the "data" object. A GraphQL
// error fails the call even when gh exits 0.
func (g *GH) mutate(ctx context.Context, op, query string, vars map[string]any, out any) error {
	f, err := os.CreateTemp("", "gg-gh-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(map[string]any{"query": query, "variables": vars}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	res, runErr := g.run(ctx, "gh api graphql ("+op+")", "api", "graphql", "--input", f.Name())
	var doc struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal([]byte(res.Stdout), &doc)
	if len(doc.Errors) > 0 {
		var msgs []string
		for _, e := range doc.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("%s: %s", op, strings.Join(msgs, "; "))
	}
	if runErr != nil {
		if msg := strings.TrimSpace(res.Stderr); msg != "" {
			return fmt.Errorf("%s: %s", op, msg)
		}
		return fmt.Errorf("%s: %w", op, runErr)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(doc.Data, out)
}

func ghSide(s model.NoteSide) string {
	if s == model.NoteSideOld {
		return "LEFT"
	}
	return "RIGHT"
}

func (g *GH) StartReview(ctx context.Context, prID, commit string) (string, error) {
	var out struct {
		R struct {
			Review struct{ ID string } `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}
	vars := map[string]any{"pr": prID}
	if commit != "" {
		vars["commit"] = commit
	}
	if err := g.mutate(ctx, "StartReview", mStartReview, vars, &out); err != nil {
		return "", err
	}
	return out.R.Review.ID, nil
}

func (g *GH) AddThread(ctx context.Context, reviewID string, t Thread) (ThreadRef, error) {
	vars := map[string]any{"review": reviewID, "path": t.Path, "body": t.Body, "subject": "FILE"}
	if t.Line > 0 {
		vars["subject"], vars["line"], vars["side"] = "LINE", t.Line, ghSide(t.Side)
		if t.StartLine > 0 && t.StartLine < t.Line {
			vars["startLine"], vars["startSide"] = t.StartLine, ghSide(t.Side)
		}
	}
	var out struct {
		R struct {
			Thread struct {
				ID       string
				Comments struct {
					Nodes []struct{ ID, URL string }
				}
			}
		} `json:"addPullRequestReviewThread"`
	}
	if err := g.mutate(ctx, "AddThread", mAddThread, vars, &out); err != nil {
		return ThreadRef{}, err
	}
	ref := ThreadRef{ID: out.R.Thread.ID}
	if n := out.R.Thread.Comments.Nodes; len(n) > 0 {
		ref.CommentID, ref.URL = n[0].ID, n[0].URL
	}
	return ref, nil
}

func (g *GH) Reply(ctx context.Context, reviewID, threadID, body string) (CommentRef, error) {
	vars := map[string]any{"thread": threadID, "body": body}
	if reviewID != "" {
		vars["review"] = reviewID
	}
	var out struct {
		R struct {
			Comment struct{ ID, URL string }
		} `json:"addPullRequestReviewThreadReply"`
	}
	if err := g.mutate(ctx, "Reply", mReply, vars, &out); err != nil {
		return CommentRef{}, err
	}
	return CommentRef{ID: out.R.Comment.ID, URL: out.R.Comment.URL}, nil
}

func (g *GH) SubmitReview(ctx context.Context, reviewID string, ev Event, body string) error {
	return g.mutate(ctx, "SubmitReview", mSubmit, map[string]any{"review": reviewID, "event": string(ev), "body": body}, nil)
}

func (g *GH) DeletePendingReview(ctx context.Context, reviewID string) error {
	return g.mutate(ctx, "DeleteReview", mDelete, map[string]any{"review": reviewID}, nil)
}

func (g *GH) Resolve(ctx context.Context, threadID string) error {
	return g.mutate(ctx, "Resolve", mResolve, map[string]any{"thread": threadID}, nil)
}

func (g *GH) Unresolve(ctx context.Context, threadID string) error {
	return g.mutate(ctx, "Unresolve", mUnresolve, map[string]any{"thread": threadID}, nil)
}
