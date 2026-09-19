package forge

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

type ghLogin struct {
	Login *string `json:"login"`
}

// name: GitHub nulls the author of a deleted account; it renders as "ghost".
func (l *ghLogin) name() string {
	if l == nil || l.Login == nil || *l.Login == "" {
		return "ghost"
	}
	return *l.Login
}

type ghPR struct {
	Number              int      `json:"number"`
	Title               string   `json:"title"`
	Body                string   `json:"body"`
	Author              *ghLogin `json:"author"`
	State               string   `json:"state"`
	IsDraft             bool     `json:"isDraft"`
	ReviewDecision      string   `json:"reviewDecision"`
	HeadRefName         string   `json:"headRefName"`
	IsCrossRepository   bool     `json:"isCrossRepository"`
	HeadRepositoryOwner *ghLogin `json:"headRepositoryOwner"`
	HeadRepository      *struct {
		Name string `json:"name"`
	} `json:"headRepository"`
	BaseRefName string    `json:"baseRefName"`
	BaseRefOid  string    `json:"baseRefOid"`
	HeadRefOid  string    `json:"headRefOid"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (g ghPR) model() model.PullRequest {
	p := model.PullRequest{
		Number: g.Number, Title: g.Title, Body: normText(g.Body), Author: g.Author.name(),
		State: strings.ToLower(g.State), Draft: g.IsDraft,
		ReviewState: strings.ToLower(g.ReviewDecision),
		Source:      g.HeadRefName, Target: g.BaseRefName,
		HeadSHA: g.HeadRefOid, BaseSHA: g.BaseRefOid, URL: g.URL,
		Created: g.CreatedAt, Updated: g.UpdatedAt,
	}
	if g.IsCrossRepository && g.HeadRepository != nil {
		p.SourceRepo = g.HeadRepositoryOwner.name() + "/" + g.HeadRepository.Name
	}
	return p
}

// normText normalises forge text: CRLF/CR → LF, surrounding blank space cut.
func normText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(strings.ReplaceAll(s, "\r", "\n"))
}

func parsePRList(b []byte) ([]model.PullRequest, error) {
	var raw []ghPR
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]model.PullRequest, 0, len(raw))
	for _, g := range raw {
		out = append(out, g.model())
	}
	return out, nil
}

func parsePR(b []byte) (model.PullRequest, error) {
	var g ghPR
	if err := json.Unmarshal(b, &g); err != nil {
		return model.PullRequest{}, err
	}
	return g.model(), nil
}

type ghPage struct {
	HasNextPage bool `json:"hasNextPage"`
}

type ghThreadComment struct {
	ID      string `json:"id"`
	ReplyTo *struct {
		ID string `json:"id"`
	} `json:"replyTo"`
	Author    *ghLogin  `json:"author"`
	Body      string    `json:"body"`
	DiffHunk  string    `json:"diffHunk"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ghThread struct {
	Path        string `json:"path"`
	Line        *int   `json:"line"`
	StartLine   *int   `json:"startLine"`
	DiffSide    string `json:"diffSide"`
	SubjectType string `json:"subjectType"`
	IsResolved  bool   `json:"isResolved"`
	IsOutdated  bool   `json:"isOutdated"`
	Comments    struct {
		PageInfo ghPage            `json:"pageInfo"`
		Nodes    []ghThreadComment `json:"nodes"`
	} `json:"comments"`
}

type ghThreads struct {
	Data struct {
		Repository struct {
			PullRequest *struct {
				ReviewThreads struct {
					PageInfo ghPage     `json:"pageInfo"`
					Nodes    []ghThread `json:"nodes"`
				} `json:"reviewThreads"`
				Comments struct {
					PageInfo ghPage `json:"pageInfo"`
					Nodes    []struct {
						ID        string    `json:"id"`
						Author    *ghLogin  `json:"author"`
						Body      string    `json:"body"`
						CreatedAt time.Time `json:"createdAt"`
						UpdatedAt time.Time `json:"updatedAt"`
					} `json:"nodes"`
				} `json:"comments"`
				Reviews struct {
					PageInfo ghPage `json:"pageInfo"`
					Nodes    []struct {
						ID          string    `json:"id"`
						Author      *ghLogin  `json:"author"`
						Body        string    `json:"body"`
						State       string    `json:"state"`
						SubmittedAt time.Time `json:"submittedAt"`
					} `json:"nodes"`
				} `json:"reviews"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// parseThreads flattens one PR's review threads, conversation comments and
// review summaries. truncated reports that some connection had a further page
// (the query is single-page by design).
func parseThreads(b []byte) ([]model.ForgeComment, bool, error) {
	var raw ghThreads
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, false, err
	}
	pr := raw.Data.Repository.PullRequest
	if pr == nil {
		return nil, false, ErrNotFound
	}
	truncated := pr.ReviewThreads.PageInfo.HasNextPage || pr.Comments.PageInfo.HasNextPage || pr.Reviews.PageInfo.HasNextPage
	var out []model.ForgeComment
	for _, th := range pr.ReviewThreads.Nodes {
		truncated = truncated || th.Comments.PageInfo.HasNextPage
		kind := model.ForgeCommentInline
		if th.SubjectType == "FILE" {
			kind = model.ForgeCommentFile
		}
		side := model.NoteSideNew
		if th.DiffSide == "LEFT" {
			side = model.NoteSideOld
		}
		line, start := 0, 0
		if th.Line != nil {
			line, start = *th.Line, *th.Line
		}
		if th.StartLine != nil {
			start = *th.StartLine
		}
		for _, c := range th.Comments.Nodes {
			fc := model.ForgeComment{
				ID: c.ID, Kind: kind, Author: c.Author.name(), Body: normText(c.Body),
				Path: th.Path, Side: side, Line: line, StartLine: start,
				Outdated: th.IsOutdated, Resolved: th.IsResolved, Hunk: c.DiffHunk,
				Created: c.CreatedAt, Updated: c.UpdatedAt,
			}
			if c.ReplyTo != nil {
				fc.ParentID = c.ReplyTo.ID
			}
			out = append(out, fc)
		}
	}
	for _, c := range pr.Comments.Nodes {
		out = append(out, model.ForgeComment{
			ID: c.ID, Kind: model.ForgeCommentGeneral, Author: c.Author.name(),
			Body: normText(c.Body), Created: c.CreatedAt, Updated: c.UpdatedAt,
		})
	}
	for _, r := range pr.Reviews.Nodes {
		verdict := strings.ToLower(r.State)
		if verdict == "pending" || (verdict == "commented" && strings.TrimSpace(r.Body) == "") {
			continue // the bare envelope of inline comments carries no news
		}
		out = append(out, model.ForgeComment{
			ID: r.ID, Kind: model.ForgeCommentReview, Author: r.Author.name(),
			Body: normText(r.Body), Verdict: verdict, Created: r.SubmittedAt, Updated: r.SubmittedAt,
		})
	}
	return out, truncated, nil
}
