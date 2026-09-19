package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// EnvBin overrides the gh binary (tests, e2e, an unusual install).
const EnvBin = "GG_GH_BIN"

const prFields = "number,title,author,state,isDraft,reviewDecision,headRefName,isCrossRepository," +
	"headRepositoryOwner,headRepository,baseRefName,baseRefOid,headRefOid,url,createdAt,updatedAt"

// threadsQuery is single-page by design: hasNextPage on any connection
// surfaces as truncated rather than a pagination loop.
const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){
reviewThreads(first:100){pageInfo{hasNextPage} nodes{path line startLine diffSide subjectType isResolved isOutdated
comments(first:50){pageInfo{hasNextPage} nodes{id replyTo{id} author{login} body diffHunk createdAt updatedAt}}}}
comments(first:100){pageInfo{hasNextPage} nodes{id author{login} body createdAt updatedAt}}
reviews(first:100){pageInfo{hasNextPage} nodes{id author{login} body state submittedAt}}}}}`

// GH reads GitHub through the system gh CLI. Every verb it runs is a read.
type GH struct{ r gitexec.Runner }

// NewGH builds the production provider. The runner is gitexec's: a generic
// subprocess runner whose binary path is a constructor argument.
func NewGH(workDir string, rec observ.Recorder) *GH {
	bin := os.Getenv(EnvBin)
	if bin == "" {
		bin = "gh"
	}
	return &GH{r: gitexec.NewExecRunner(bin, workDir, rec)}
}

// NewGHWithRunner injects the runner (argv tests use a FakeRunner).
func NewGHWithRunner(r gitexec.Runner) *GH { return &GH{r: r} }

func (g *GH) Name() string             { return "github" }
func (g *GH) HeadRefspec(n int) string { return "refs/pull/" + strconv.Itoa(n) + "/head" }

func (g *GH) run(ctx context.Context, name string, argv ...string) (gitexec.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	return g.r.Run(ctx, name, argv)
}

func (g *GH) Detect(ctx context.Context) error {
	_, err := g.run(ctx, "gh pr list (detect)", "pr", "list", "--limit", "1", "--json", "number")
	return err
}

func (g *GH) ListOpen(ctx context.Context) ([]model.PullRequest, error) {
	res, err := g.run(ctx, "gh pr list", "pr", "list", "--state", "open", "--limit", "100", "--json", prFields)
	if err != nil {
		return nil, err
	}
	return parsePRList([]byte(res.Stdout))
}

func (g *GH) PR(ctx context.Context, n int) (model.PullRequest, error) {
	res, err := g.run(ctx, "gh pr view", "pr", "view", strconv.Itoa(n), "--json", prFields+",body")
	if err != nil {
		if strings.Contains(res.Stderr+err.Error(), "Could not resolve to a PullRequest") {
			return model.PullRequest{}, fmt.Errorf("#%d: %w", n, ErrNotFound)
		}
		return model.PullRequest{}, err
	}
	return parsePR([]byte(res.Stdout))
}

func (g *GH) Comments(ctx context.Context, n int) ([]model.ForgeComment, bool, error) {
	// {owner}/{repo} are gh's own placeholders, filled from its repo resolution.
	res, err := g.run(ctx, "gh api graphql (threads)", "api", "graphql",
		"-F", "owner={owner}", "-F", "name={repo}", "-F", "number="+strconv.Itoa(n),
		"-f", "query="+threadsQuery)
	if err != nil {
		return nil, false, err
	}
	return parseThreads([]byte(res.Stdout))
}

func (g *GH) BaseRepo(ctx context.Context) (string, string, error) {
	res, err := g.run(ctx, "gh repo view", "repo", "view", "--json", "nameWithOwner,url,sshUrl")
	if err != nil {
		return "", "", err
	}
	var v struct {
		URL    string `json:"url"`
		SSHURL string `json:"sshUrl"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &v); err != nil {
		return "", "", err
	}
	fallback := v.SSHURL
	if fallback == "" {
		fallback = v.URL
	}
	return RepoSlug(v.URL), fallback, nil
}
