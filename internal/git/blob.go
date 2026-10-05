package git

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
)

// Working-file blob ids, computed in-process (spec 2026-10-04 working
// reviews §5): no `git hash-object` per file. The id is git's — "blob <n>\0"
// + the bytes, in the repo's object format — of the WORKING-TREE bytes, so
// it is never compared with an index blob (clean filters, autocrlf).

// ErrNotAFile is a path that is neither a regular file nor a symlink (a
// directory, e.g. a nested repository `ls-files --others` lists as "sub/").
var ErrNotAFile = errors.New("not a regular file")

// WorktreeBlob is a working file's blob id plus what a numstat line needs.
type WorktreeBlob struct {
	ID     string
	Lines  int  // newline-terminated lines, plus a final unterminated one
	Binary bool // git's heuristic: a NUL in the first 8000 bytes
}

// binarySniff is how far git looks for a NUL to call a file binary.
const binarySniff = 8000

func objectHash(format string) hash.Hash {
	if format == "sha256" {
		return sha256.New()
	}
	return sha1.New()
}

// BlobFormatOf names the object format a blob id was hashed in, by length.
func BlobFormatOf(id string) string {
	if len(id) == 64 {
		return "sha256"
	}
	return "sha1"
}

// blobStats counts lines and sniffs for binary over a stream.
type blobStats struct {
	seen   int64
	nl     int
	last   byte
	binary bool
}

func (s *blobStats) Write(p []byte) (int, error) {
	if s.seen < binarySniff {
		head := p
		if rest := binarySniff - s.seen; int64(len(head)) > rest {
			head = head[:rest]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			s.binary = true
		}
	}
	s.nl += bytes.Count(p, []byte{'\n'})
	if len(p) > 0 {
		s.last = p[len(p)-1]
	}
	s.seen += int64(len(p))
	return len(p), nil
}

func (s *blobStats) lines() int {
	if s.seen > 0 && s.last != '\n' {
		return s.nl + 1
	}
	return s.nl
}

// BlobOf is data's blob id in format, with its line stats.
func BlobOf(format string, data []byte) WorktreeBlob {
	h := objectHash(format)
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	var st blobStats
	st.Write(data)
	return WorktreeBlob{ID: hex.EncodeToString(h.Sum(nil)), Lines: st.lines(), Binary: st.binary}
}

// HashWorktreeFile hashes the file at path (absolute) as git would store it:
// a symlink as its target string, a regular file as its bytes — streamed, so
// a huge file is never held whole. Anything else is ErrNotAFile; an absent
// path is the OS's not-exist error.
func HashWorktreeFile(format, path string) (WorktreeBlob, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return WorktreeBlob{}, err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return WorktreeBlob{}, err
		}
		return BlobOf(format, []byte(target)), nil
	case !fi.Mode().IsRegular():
		return WorktreeBlob{}, ErrNotAFile
	}
	f, err := os.Open(path)
	if err != nil {
		return WorktreeBlob{}, err
	}
	defer f.Close()
	h := objectHash(format)
	fmt.Fprintf(h, "blob %d\x00", fi.Size())
	var st blobStats
	n, err := io.Copy(io.MultiWriter(h, &st), f)
	if err != nil {
		return WorktreeBlob{}, err
	}
	if n != fi.Size() {
		return WorktreeBlob{}, fmt.Errorf("%s changed while it was read", path)
	}
	return WorktreeBlob{ID: hex.EncodeToString(h.Sum(nil)), Lines: st.lines(), Binary: st.binary}, nil
}
