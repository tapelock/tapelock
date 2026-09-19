// Package cassette stores and retrieves recorded interactions as JSONL
// files (one file per cassette, one line per interaction). No database, no
// external index — Append/Lookup/Read over a git-friendly append-only file.
package cassette

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// maxLineSize bounds how large a single recorded interaction line may be.
// Chat completion bodies are typically kilobytes; this leaves generous
// headroom without letting a corrupt file exhaust memory on read.
const maxLineSize = 64 * 1024 * 1024

// Manager reads and appends interactions in a single JSONL cassette file.
// There is no database and no external index: the whole cassette is loaded
// into memory once, and Lookup is a slice index by hash built at load time
// — fine at the scale a single test suite records (see mvp.md §5).
//
// A Manager is safe for concurrent use.
type Manager struct {
	path string

	mu           sync.Mutex
	interactions []Interaction
	byHash       map[string][]int // RequestHash -> indexes into interactions, in file order
	cursor       map[string]int   // RequestHash -> next index into byHash[hash] to serve
}

// NewManager loads path into memory. A missing file is not an error: it is
// treated as an empty cassette, which Append will create on first write.
//
// If any line fails to decode, the whole cassette is rejected instead of
// silently dropping the bad entry — an unreadable cassette must fail the
// build, not degrade into a partial replay (see the PRD's "cassette
// illisible = échec" principle).
func NewManager(path string) (*Manager, error) {
	m := &Manager{
		path:   path,
		byHash: make(map[string][]int),
		cursor: make(map[string]int),
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cassette: open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			return nil, fmt.Errorf("cassette: %s: empty line %d (truncated or corrupt cassette)", path, lineNum)
		}
		it, err := DecodeLine(line)
		if err != nil {
			return nil, fmt.Errorf("cassette: %s: line %d: %w", path, lineNum, err)
		}
		m.index(it)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cassette: %s: %w", path, err)
	}

	return m, nil
}

// index records it in the in-memory lookup structures. Callers must hold m.mu.
func (m *Manager) index(it Interaction) {
	i := len(m.interactions)
	m.interactions = append(m.interactions, it)
	m.byHash[it.RequestHash] = append(m.byHash[it.RequestHash], i)
}

// Read returns every interaction currently loaded, in recording order.
func (m *Manager) Read() []Interaction {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Interaction, len(m.interactions))
	copy(out, m.interactions)
	return out
}

// Lookup returns the next not-yet-served interaction recorded for hash, in
// the order it was originally recorded. Calling Lookup again with the same
// hash returns the next occurrence, so an agent loop that repeats an
// identical request replays a different response each time. ok is false
// once every recorded occurrence for hash has been served.
func (m *Manager) Lookup(hash string) (Interaction, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	indexes := m.byHash[hash]
	next := m.cursor[hash]
	if next >= len(indexes) {
		return Interaction{}, false
	}

	m.cursor[hash] = next + 1
	return m.interactions[indexes[next]], true
}

// Append writes it to the cassette file and to the in-memory index, creating
// the parent directory and the file itself if needed.
//
// It performs a single write(2) of the encoded line plus a trailing
// newline in O_APPEND mode: on the local filesystems Tapelock targets, a
// crash mid-write leaves the file exactly as it was before the call, never
// a partial line, so a reader never has to guess whether the last line is
// finished.
func (m *Manager) Append(it Interaction) error {
	line, err := EncodeLine(it)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	m.mu.Lock()
	defer m.mu.Unlock()

	if dir := filepath.Dir(m.path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cassette: create %s: %w", dir, err)
		}
	}

	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("cassette: open %s: %w", m.path, err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("cassette: append to %s: %w", m.path, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("cassette: sync %s: %w", m.path, err)
	}

	m.index(it)
	return nil
}
