package tui

import "testing"

// Manual remoteTagsMsg stores the set.
func TestRemoteTagsMsgStoresSet(t *testing.T) {
	m := Model{}
	u, _ := m.Update(remoteTagsMsg{names: map[string]bool{"v1": true}, manual: true})
	m = u.(Model)
	if !m.remoteTagNames["v1"] {
		t.Fatal("manual remoteTagsMsg should store the name set")
	}
}

// Manual error surfaces on the status line and leaves the set unchanged.
func TestRemoteTagsMsgManualErrorStatus(t *testing.T) {
	m := Model{remoteTagNames: map[string]bool{"old": true}}
	u, _ := m.Update(remoteTagsMsg{err: errTestRemote, manual: true})
	m = u.(Model)
	if m.statusMsg == "" {
		t.Fatal("manual error should set a status message")
	}
	if !m.remoteTagNames["old"] {
		t.Fatal("error must not clear the existing set")
	}
}

// Optimistic add on PushTag success; remove on DeleteRemoteTag success.
func TestOptimisticRemoteTagAdd(t *testing.T) {
	m := Model{pendingRemoteTagSet: "v9"}
	m = m.applyPendingRemoteTag() // success-path helper
	if !m.remoteTagNames["v9"] {
		t.Fatal("push success should add the tag to the remote set")
	}
}
func TestOptimisticRemoteTagRemove(t *testing.T) {
	m := Model{remoteTagNames: map[string]bool{"v9": true}, pendingRemoteTagUnset: "v9"}
	m = m.applyPendingRemoteTag()
	if m.remoteTagNames["v9"] {
		t.Fatal("delete-remote success should drop the tag from the remote set")
	}
}

var errTestRemote = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }
