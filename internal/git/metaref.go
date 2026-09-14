package git

import (
	"context"
	"strconv"
	"strings"
)

// MetaRefPrefix is the namespace of gg store-format markers:
// refs/gg/meta/<store>/<format>. The format number lives in the REF NAME so a
// single for-each-ref reads every marker — no blob writes, no cat-file. Like
// refs/gg/versions/, it sits outside refs/heads|tags|remotes, so it is never
// pushed or fetched and is shared by all worktrees via the common dir.
const MetaRefPrefix = "refs/gg/meta/"

// MetaRef builds the marker ref for store at format.
func MetaRef(store string, format int) string {
	return MetaRefPrefix + store + "/" + strconv.Itoa(format)
}

// ParseMetaRef splits a marker ref back into store and format. Store names
// never contain "/", so the split is on the last separator.
func ParseMetaRef(ref string) (store string, format int, ok bool) {
	rest, found := strings.CutPrefix(ref, MetaRefPrefix)
	if !found {
		return "", 0, false
	}
	i := strings.LastIndex(rest, "/")
	if i <= 0 || i == len(rest)-1 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil {
		return "", 0, false
	}
	return rest[:i], n, true
}

// StoreFormats reads every marker in one invocation. A store with no marker is
// simply absent from the map; the caller resolves that against whether the
// store holds data (see preflight.DataFormat).
func (r *Repo) StoreFormats(ctx context.Context) (map[string]int, error) {
	infos, err := r.ForEachRef(ctx, MetaRefPrefix)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(infos))
	for _, info := range infos {
		store, format, ok := ParseMetaRef(info.Ref)
		if !ok {
			continue
		}
		// Defensive: a repo carrying two markers for one store keeps the
		// higher, so a half-finished migration reads as the newer format
		// rather than silently as the older one.
		if format > out[store] {
			out[store] = format
		}
	}
	return out, nil
}

// StampStoreFormat records store at format, removing any LOWER marker for that
// store. Called by a store's WRITER on first write — never at startup.
//
// A HIGHER marker always survives: it means a newer gg wrote this store, and
// the contract is that data written by a newer gg is never rewritten (nor its
// marker downgraded) by an older build. This mirrors StoreFormats' higher-wins
// reconciliation — an older build whose writes still land must not be able to
// relabel format-2 data as format 1.
func (r *Repo) StampStoreFormat(ctx context.Context, store string, format int) error {
	sha, err := r.EmptyTree(ctx)
	if err != nil {
		return err
	}
	if err := r.UpdateRef(ctx, MetaRef(store, format), sha); err != nil {
		return err
	}
	infos, err := r.ForEachRef(ctx, MetaRefPrefix+store+"/")
	if err != nil {
		return nil // the marker is written; pruning stale ones is best-effort
	}
	for _, info := range infos {
		s, f, ok := ParseMetaRef(info.Ref)
		if !ok || s != store || f >= format {
			continue
		}
		_ = r.DeleteRef(ctx, info.Ref)
	}
	return nil
}
