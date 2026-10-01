package domain

import "github.com/homeend/gigagit/internal/wtclaim"

// liveClaim reports gitDir's claim (Task 7 adds the dead-claim sweep).
func liveClaim(gitDir string, lv liveView) (ClaimInfo, bool) {
	if gitDir == "" {
		return ClaimInfo{}, false
	}
	c, ok, err := wtclaim.Read(gitDir)
	if err != nil || !ok {
		return ClaimInfo{}, false
	}
	return ClaimInfo{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note}, true
}
