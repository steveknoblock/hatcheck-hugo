package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Test helpers ---

// newArchive builds an in-memory Archive from fake CAS objects (hash ->
// content) and log entries, the way a real export would present them.
func newArchive(t *testing.T, objects map[string]string, entries []map[string]interface{}) *Archive {
	t.Helper()
	a := &Archive{
		Manifest: Manifest{Version: supportedManifestVersion},
		Objects:  make(map[string][]byte, len(objects)),
	}
	for hash, content := range objects {
		a.Objects[hash] = []byte(content)
	}
	for _, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal log entry: %v", err)
		}
		a.Log = append(a.Log, raw)
	}
	return a
}

// nameEntry builds a log entry for a name-create operation.
func nameEntry(label, hash string) map[string]interface{} {
	return nameEntryAt(label, hash, time.Now().UTC())
}

// nameEntryAt is like nameEntry but with an explicit timestamp, for tests
// that need to assert a specific date in front matter.
func nameEntryAt(label, hash string, created time.Time) map[string]interface{} {
	payload, _ := json.Marshal(map[string]string{"label": label, "hash": hash})
	return map[string]interface{}{
		"op":      "name-create",
		"created": created,
		"payload": json.RawMessage(payload),
	}
}

// stashEntry builds a log entry for a stash operation.
func stashEntry(hash string, tags []string) map[string]interface{} {
	payload, _ := json.Marshal(map[string]interface{}{"hash": hash, "size": 10, "tags": tags})
	return map[string]interface{}{
		"op":      "stash",
		"created": time.Now().UTC(),
		"payload": json.RawMessage(payload),
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

func render(t *testing.T, a *Archive, namespace string) string {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "out")
	if err := Render(a, namespace, outDir); err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	return outDir
}

// --- Render ---

func TestRender_UnknownNamespace(t *testing.T) {
	a := newArchive(t, nil, nil)
	outDir := filepath.Join(t.TempDir(), "out")

	if err := Render(a, "nonexistent", outDir); err == nil {
		t.Error("expected error for unknown namespace, got nil")
	}
}

func TestRender_LeafBecomesFile(t *testing.T) {
	hash := "aabbcc001122334455667788990011aa"
	created := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	a := newArchive(t,
		map[string]string{hash: "Hello, world. #ideas #draft"},
		[]map[string]interface{}{
			stashEntry(hash, []string{"ideas", "draft"}),
			nameEntryAt("bob/hello world", hash, created),
		})

	outDir := render(t, a, "bob")

	path := filepath.Join(outDir, "hello-world.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}

	got := readFile(t, path)
	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("expected front matter block, got:\n%s", got)
	}
	if !strings.Contains(got, `title: "hello world"`) {
		t.Errorf("expected title in front matter, got:\n%s", got)
	}
	if !strings.Contains(got, "date: 2026-03-15T12:00:00Z") {
		t.Errorf("expected date in front matter, got:\n%s", got)
	}
	if !strings.Contains(got, `tags: ["ideas", "draft"]`) {
		t.Errorf("expected tags in front matter, got:\n%s", got)
	}
	if !strings.Contains(got, "Hello, world. #ideas #draft") {
		t.Errorf("expected body content, got:\n%s", got)
	}
}

func TestRender_LatestNameUpdateWins(t *testing.T) {
	oldHash := "aabbcc001122334455667788990011aa"
	newHash := "bbccdd112233445566778899001122bb"

	update := nameEntryAt("bob/draft", newHash, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	update["op"] = "name-update"

	a := newArchive(t,
		map[string]string{oldHash: "first version", newHash: "second version"},
		[]map[string]interface{}{
			nameEntryAt("bob/draft", oldHash, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
			update,
		})

	outDir := render(t, a, "bob")

	got := readFile(t, filepath.Join(outDir, "draft.md"))
	if !strings.Contains(got, "second version") || strings.Contains(got, "first version") {
		t.Errorf("expected the updated name's content only, got:\n%s", got)
	}
	if !strings.Contains(got, "date: 2026-04-01T00:00:00Z") {
		t.Errorf("expected the update's date, got:\n%s", got)
	}
}

func TestRender_OtherNamespacesIgnored(t *testing.T) {
	mine := "aabbcc001122334455667788990011aa"
	theirs := "bbccdd112233445566778899001122bb"

	a := newArchive(t,
		map[string]string{mine: "mine", theirs: "theirs"},
		[]map[string]interface{}{
			nameEntry("bob/mine", mine),
			nameEntry("alice/theirs", theirs),
		})

	outDir := render(t, a, "bob")

	if _, err := os.Stat(filepath.Join(outDir, "mine.md")); err != nil {
		t.Errorf("expected mine.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "theirs.md")); err == nil {
		t.Error("did not expect another namespace's name to be rendered")
	}
}

func TestRender_CollectionBecomesDirectory(t *testing.T) {
	collectionHash := "cc00112233445566778899001122ccdd"
	leaf1 := "aabbcc001122334455667788990011aa"
	leaf2 := "bbccdd112233445566778899001122bb"

	a := newArchive(t,
		map[string]string{
			leaf1:          "first line",
			leaf2:          "second line",
			collectionHash: `["` + leaf1 + `","` + leaf2 + `"]`,
		},
		[]map[string]interface{}{
			stashEntry(leaf1, nil),
			stashEntry(leaf2, nil),
			nameEntry("bob/haiku", collectionHash),
		})

	outDir := render(t, a, "bob")

	haikuDir := filepath.Join(outDir, "haiku")
	info, err := os.Stat(haikuDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected %s to be a directory: %v", haikuDir, err)
	}

	indexContent := readFile(t, filepath.Join(haikuDir, "_index.md"))
	if !strings.Contains(indexContent, `title: "haiku"`) {
		t.Errorf("expected _index.md to have collection's title, got:\n%s", indexContent)
	}

	// Anonymous members (no Name of their own) fall back to shortened hash.
	if content := readFile(t, filepath.Join(haikuDir, leaf1[:8]+".md")); !strings.Contains(content, "first line") {
		t.Errorf("expected member1 content, got:\n%s", content)
	}
	if content := readFile(t, filepath.Join(haikuDir, leaf2[:8]+".md")); !strings.Contains(content, "second line") {
		t.Errorf("expected member2 content, got:\n%s", content)
	}
}

func TestRender_AnonymousMemberHasNoFrontMatter(t *testing.T) {
	collectionHash := "cc00112233445566778899001122ccdd"
	leaf := "aabbcc001122334455667788990011aa"

	a := newArchive(t,
		map[string]string{
			leaf:           "anonymous content #shouldnotappear",
			collectionHash: `["` + leaf + `"]`,
		},
		[]map[string]interface{}{
			stashEntry(leaf, []string{"shouldnotappear"}),
			nameEntry("bob/doc", collectionHash),
		})

	outDir := render(t, a, "bob")

	got := readFile(t, filepath.Join(outDir, "doc", leaf[:8]+".md"))
	if strings.HasPrefix(got, "---\n") {
		t.Errorf("expected no front matter for anonymous member, got:\n%s", got)
	}
	if !strings.Contains(got, "anonymous content #shouldnotappear") {
		t.Errorf("expected plain content preserved, got:\n%s", got)
	}
}

func TestRender_NamedCollectionMemberUsesItsOwnName(t *testing.T) {
	collectionHash := "cc00112233445566778899001122ccdd"
	leaf := "aabbcc001122334455667788990011aa"
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := newArchive(t,
		map[string]string{
			leaf:           "a poem #haiku",
			collectionHash: `["` + leaf + `"]`,
		},
		[]map[string]interface{}{
			stashEntry(leaf, []string{"haiku"}),
			nameEntryAt("bob/cicada song", leaf, created), // leaf is ALSO its own top-level name
			nameEntry("bob/collected works", collectionHash),
		})

	outDir := render(t, a, "bob")

	// As a top-level name, it's its own file...
	if _, err := os.Stat(filepath.Join(outDir, "cicada-song.md")); err != nil {
		t.Fatalf("expected top-level cicada-song.md to exist: %v", err)
	}

	// ...and reused inside the collection, it should use its real name,
	// not fall back to a shortened hash, and carry front matter.
	got := readFile(t, filepath.Join(outDir, "collected-works", "cicada-song.md"))
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected front matter for a named member, got:\n%s", got)
	}
	if !strings.Contains(got, `title: "cicada song"`) {
		t.Errorf("expected member's own title, got:\n%s", got)
	}
	if !strings.Contains(got, "a poem #haiku") {
		t.Errorf("expected content, got:\n%s", got)
	}

	fallbackPath := filepath.Join(outDir, "collected-works", leaf[:8]+".md")
	if _, err := os.Stat(fallbackPath); err == nil {
		t.Errorf("did not expect a shortened-hash fallback file for a named member: %s", fallbackPath)
	}
}

func TestRender_RelationGetsDescriptiveFile(t *testing.T) {
	relationHash := "dd00112233445566778899001122ddee"
	from := "aabbcc001122334455667788990011aa"
	to := "bbccdd112233445566778899001122bb"

	a := newArchive(t,
		map[string]string{
			from:         "source doc",
			to:           "target doc",
			relationHash: `{"from":"` + from + `","rel":"contextualizes","to":"` + to + `"}`,
		},
		[]map[string]interface{}{
			stashEntry(from, nil),
			stashEntry(to, nil),
			nameEntry("bob/source doc", from),
			nameEntry("bob/target doc", to),
			nameEntry("bob/the link", relationHash),
		})

	outDir := render(t, a, "bob")

	got := readFile(t, filepath.Join(outDir, "the-link.md"))
	if !strings.Contains(got, "Relation: contextualizes") {
		t.Errorf("expected relation type in body, got:\n%s", got)
	}
	if !strings.Contains(got, "From: source doc") || !strings.Contains(got, "To: target doc") {
		t.Errorf("expected from/to resolved to their names, got:\n%s", got)
	}
}

func TestRender_SlugCollisionGetsSuffix(t *testing.T) {
	hash1 := "aabbcc001122334455667788990011aa"
	hash2 := "bbccdd112233445566778899001122bb"

	a := newArchive(t,
		map[string]string{hash1: "first", hash2: "second"},
		[]map[string]interface{}{
			// Same label modulo punctuation -> same slug "my-doc".
			nameEntry("bob/My Doc!", hash1),
			nameEntry("bob/my doc?", hash2),
		})

	outDir := render(t, a, "bob")

	if _, err := os.Stat(filepath.Join(outDir, "my-doc.md")); err != nil {
		t.Errorf("expected my-doc.md to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "my-doc-2.md")); err != nil {
		t.Errorf("expected collision suffix my-doc-2.md to exist: %v", err)
	}
}

func TestRender_SharedContentWrittenAtEveryLocation(t *testing.T) {
	sharedLeaf := "aabbcc001122334455667788990011aa"
	collectionA := "cc00112233445566778899001122ccdd"
	collectionB := "dd00112233445566778899001122ddee"

	a := newArchive(t,
		map[string]string{
			sharedLeaf:  "shared content",
			collectionA: `["` + sharedLeaf + `"]`,
			collectionB: `["` + sharedLeaf + `"]`,
		},
		[]map[string]interface{}{
			nameEntry("bob/collection-a", collectionA),
			nameEntry("bob/collection-b", collectionB),
		})

	outDir := render(t, a, "bob")

	// The same anonymous hash is a member of two unrelated collections;
	// a markdown file tree needs real content at both locations.
	for _, dir := range []string{"collection-a", "collection-b"} {
		got := readFile(t, filepath.Join(outDir, dir, sharedLeaf[:8]+".md"))
		if !strings.Contains(got, "shared content") {
			t.Errorf("expected shared content in %s, got:\n%s", dir, got)
		}
	}
}

func TestRender_MissingObjectIsAnError(t *testing.T) {
	hash := "aabbcc001122334455667788990011aa"
	a := newArchive(t, nil, []map[string]interface{}{nameEntry("bob/ghost", hash)})

	err := Render(a, "bob", filepath.Join(t.TempDir(), "out"))
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected an error naming the failing entry, got %v", err)
	}
}

func TestRender_ShortHashDoesNotPanic(t *testing.T) {
	collectionHash := "cc00112233445566778899001122ccdd"
	a := newArchive(t,
		map[string]string{
			"abc":          "tiny hash content",
			collectionHash: `["abc"]`,
		},
		[]map[string]interface{}{nameEntry("bob/odd", collectionHash)})

	outDir := render(t, a, "bob")

	if got := readFile(t, filepath.Join(outDir, "odd", "abc.md")); !strings.Contains(got, "tiny hash content") {
		t.Errorf("expected content for short-hash member, got:\n%s", got)
	}
}

// --- Helpers ---

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"browsing the shelves": "browsing-the-shelves",
		"A book gathers dust":  "a-book-gathers-dust",
		"Haiku":                "haiku",
		"  extra   spaces  ":   "extra-spaces",
		"already-slugged_ok":   "already-slugged_ok",
	}
	for input, want := range cases {
		if got := slug(input); got != want {
			t.Errorf("slug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseTagsForExport(t *testing.T) {
	got := parseTagsForExport("Hello #World, this is #ideas and #World again")
	want := []string{"world", "ideas"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %v, got %v", want, got)
		}
	}
}
