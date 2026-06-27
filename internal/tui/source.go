package tui

// sourceKey identifies one independently-refreshable data source. Each maps to a
// single gated domain query and feeds one or more panels (see srcConsumers). It
// is the unit of the reactive refresh layer: a read of a source emits a
// dataAvailableMsg, and every consuming panel re-renders from the stored value.
type sourceKey int

const (
	srcStatus sourceKey = iota
	srcBranches
	srcRemotes
	srcTags
	srcReflog
	srcWorktrees
	srcFeed
	srcIdentity
	srcCount
)

// srcConsumers maps a source to the panels that render from it. Used to target
// the manual-refresh spinner and, in Phase B, to decide which sources a timer
// polls. srcIdentity feeds the Settings popup, not a left panel, so it is absent.
var srcConsumers = map[sourceKey][]panel{
	srcStatus:    {panelFiles, panelStaged},
	srcBranches:  {panelBranches, panelCommits},
	srcRemotes:   {panelRemotes},
	srcTags:      {panelTags},
	srcReflog:    {panelReflog},
	srcWorktrees: {panelWorktrees, panelBranches},
	srcFeed:      {panelCommits},
}

// dataAvailableMsg is the single event every source read produces. value is
// typed per source and asserted in the handler; gen ties the result to the read
// that issued it (stale gens are dropped); manual=true means a user-initiated
// read whose spinner must be cleared on arrival (false = silent, Phase B).
type dataAvailableMsg struct {
	source sourceKey
	gen    int
	value  any
	manual bool
	err    error
}
