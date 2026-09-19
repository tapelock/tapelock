package cassette

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func withHash(it Interaction, hash string) Interaction {
	it.RequestHash = hash
	return it
}

func withID(it Interaction, id string) Interaction {
	it.ID = id
	return it
}

func TestManagerNewManagerOnMissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if got := m.Read(); len(got) != 0 {
		t.Fatalf("Read() on a missing file: got %d interactions, want 0", len(got))
	}
}

func TestManagerAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	first := withID(sampleInteraction(), "id-1")
	second := withID(sampleStreamInteraction(), "id-2")

	if err := m.Append(first); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := m.Append(second); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := m.Read()
	if len(got) != 2 {
		t.Fatalf("Read(): got %d interactions, want 2", len(got))
	}
	if got[0].ID != "id-1" || got[1].ID != "id-2" {
		t.Fatalf("Read() out of order: got IDs %q, %q", got[0].ID, got[1].ID)
	}
}

func TestManagerAppendPersistsAcrossManagerInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")

	a, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := a.Append(sampleInteraction()); err != nil {
		t.Fatalf("Append: %v", err)
	}

	b, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager (reopen): %v", err)
	}

	got := b.Read()
	if len(got) != 1 {
		t.Fatalf("reopened cassette: got %d interactions, want 1", len(got))
	}
}

func TestManagerAppendCreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "cassette.jsonl")

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Append(sampleInteraction()); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cassette file was not created: %v", err)
	}
}

func TestManagerLookupServesOccurrencesInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	const hash = "sha256:repeat"
	for i, id := range []string{"first", "second", "third"} {
		it := withID(withHash(sampleInteraction(), hash), id)
		it.Response.Body = id // distinguish the three recorded responses
		if err := m.Append(it); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	for _, want := range []string{"first", "second", "third"} {
		got, ok := m.Lookup(hash)
		if !ok {
			t.Fatalf("Lookup(%q): want hit for occurrence %q, got miss", hash, want)
		}
		if got.ID != want {
			t.Fatalf("Lookup(%q): got occurrence %q, want %q", hash, got.ID, want)
		}
	}

	if _, ok := m.Lookup(hash); ok {
		t.Fatalf("Lookup(%q): want miss after all occurrences served, got a hit", hash)
	}
}

func TestManagerLookupMissingHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if _, ok := m.Lookup("sha256:never-recorded"); ok {
		t.Fatal("Lookup: want miss for a hash that was never recorded, got a hit")
	}
}

func TestManagerLookupKeepsHashesIndependent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Interleave two different hashes to make sure each has its own cursor.
	if err := m.Append(withID(withHash(sampleInteraction(), "sha256:a"), "a1")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := m.Append(withID(withHash(sampleInteraction(), "sha256:b"), "b1")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := m.Append(withID(withHash(sampleInteraction(), "sha256:a"), "a2")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	for _, want := range []string{"a1", "a2"} {
		got, ok := m.Lookup("sha256:a")
		if !ok || got.ID != want {
			t.Fatalf("Lookup(sha256:a): got (%v, %v), want (%q, true)", got.ID, ok, want)
		}
	}

	got, ok := m.Lookup("sha256:b")
	if !ok || got.ID != "b1" {
		t.Fatalf("Lookup(sha256:b): got (%v, %v), want (\"b1\", true)", got.ID, ok)
	}
}

func TestManagerNewManagerRejectsCorruptCassette(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	if err := os.WriteFile(path, []byte("not json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := NewManager(path); err == nil {
		t.Fatal("NewManager: want error for a corrupt cassette, got nil")
	}
}

func TestManagerNewManagerRejectsEmptyLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	line, err := EncodeLine(sampleInteraction())
	if err != nil {
		t.Fatalf("EncodeLine: %v", err)
	}
	content := append(line, "\n\n"...) // a blank line follows a valid one
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := NewManager(path); err == nil {
		t.Fatal("NewManager: want error for a cassette with a blank line, got nil")
	}
}

func TestManagerAppendIsConcurrencySafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			it := withID(sampleInteraction(), fmt.Sprintf("id-%d", i))
			if err := m.Append(it); err != nil {
				t.Errorf("Append: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := m.Read(); len(got) != n {
		t.Fatalf("Read(): got %d interactions, want %d", len(got), n)
	}

	// The file on disk must also be exactly n well-formed lines: this is
	// what actually proves concurrent Append calls never interleaved bytes.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if _, err := DecodeLine(scanner.Bytes()); err != nil {
			t.Fatalf("line %d is not valid: %v", lines+1, err)
		}
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if lines != n {
		t.Fatalf("cassette file has %d lines, want %d", lines, n)
	}
}
