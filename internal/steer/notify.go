package steer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// httpTimeout bounds a POST to an open gg web page. The endpoint answers 202
// immediately, so anything slower is a page that is already gone.
const httpTimeout = 2 * time.Second

// PostHTTP hands one command to an open gg web page. Content-Type is JSON and
// no Origin header is sent, which the server's writeGuard accepts from a
// non-browser client. A 409 means an operation is in flight and the page
// refused the command outright.
func PostHTTP(base string, c Command) error {
	if base == "" {
		return errors.New("the gg web presence carries no URL")
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/api/session/steer", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return errors.New("operation in flight")
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("gg web answered %s", resp.Status)
	}
	return nil
}

// NotifyReload tells whatever sessions are live for this inbox that the named
// sources changed. It is BEST-EFFORT and no-wait in every sense: no reply is
// requested (nothing would read one), every failure is swallowed, and a caller
// with no inbox, no live session or no network passes through untouched. A note
// verb must never fail, block, or print because of a window it does not own.
func NotifyReload(dir string, sources ...string) {
	if dir == "" || len(sources) == 0 {
		return
	}
	c := Command{Cmd: "reload", Sources: sources}
	if _, ok := Live(dir, TUIPresence); ok {
		_, _ = Post(dir, c)
	}
	if p, ok := Live(dir, WebPresence); ok {
		wc := c
		wc.ID = NewID()
		_ = PostHTTP(p.URL, wc)
	}
}
