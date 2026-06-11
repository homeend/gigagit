package tui

import "testing"

func TestPullKeyStartsOperation(t *testing.T) {
	m := loadedModel(t)
	updated, cmd := m.Update(keyMsg("p"))
	mm := updated.(Model)
	if !mm.running {
		t.Fatal("pressing p should start an operation (running=true)")
	}
	if cmd == nil {
		t.Fatal("expected a waitForOp command")
	}
	driveOp(t, mm, cmd) // drain so the goroutine doesn't leak
}

func TestKeysIgnoredWhileRunning(t *testing.T) {
	m := loadedModel(t)
	m.running = true // pretend an op is in flight
	updated, _ := m.Update(keyMsg("u"))
	mm := updated.(Model)
	if mm.opMsgs != nil {
		t.Fatal("operation keys must be ignored while another op is running")
	}
}
