package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestArchive writes a tar.gz in the layout Hatcheck's export produces:
// manifest.json, objects/<2 chars>/<rest>, metadata/log.json (a JSON array).
// manifest may be nil to omit manifest.json; entries may be nil to omit the log.
func writeTestArchive(t *testing.T, path string, manifest *Manifest, objects map[string]string, entries []map[string]interface{}) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	add := func(name string, data []byte) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}

	if manifest != nil {
		data, _ := json.MarshalIndent(manifest, "", "  ")
		add("manifest.json", data)
	}
	for hash, content := range objects {
		add("objects/"+hash[:2]+"/"+hash[2:], []byte(content))
	}
	if entries != nil {
		data, _ := json.MarshalIndent(entries, "", "  ")
		add("metadata/log.json", data)
	}
	// A file the format doesn't define: must be ignored, not rejected.
	add("README.txt", []byte("not part of the format"))

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReadArchiveFile_RoundTrip(t *testing.T) {
	hash := "aabbcc001122334455667788990011aa"
	path := filepath.Join(t.TempDir(), "bob.tar.gz")
	writeTestArchive(t, path,
		&Manifest{Source: "bob", Version: "1", Objects: 1, Namespace: "bob"},
		map[string]string{hash: "hello"},
		[]map[string]interface{}{nameEntry("bob/hi", hash)})

	a, err := ReadArchiveFile(path)
	if err != nil {
		t.Fatalf("ReadArchiveFile() error: %v", err)
	}
	if a.Manifest.Namespace != "bob" || a.Manifest.Source != "bob" {
		t.Errorf("unexpected manifest: %+v", a.Manifest)
	}
	got, err := a.Object(hash)
	if err != nil || got != "hello" {
		t.Errorf("Object(%s) = %q, %v; want \"hello\"", hash, got, err)
	}
	if len(a.Log) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(a.Log))
	}
}

func TestReadArchiveFile_ObjectLookupMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.tar.gz")
	writeTestArchive(t, path, &Manifest{Version: "1"}, nil, nil)

	a, err := ReadArchiveFile(path)
	if err != nil {
		t.Fatalf("ReadArchiveFile() error: %v", err)
	}
	if _, err := a.Object("deadbeef"); err == nil {
		t.Error("expected an error for an object that is not in the archive")
	}
}

func TestReadArchiveFile_MissingManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nomanifest.tar.gz")
	writeTestArchive(t, path, nil, nil, nil)

	_, err := ReadArchiveFile(path)
	if err == nil || !strings.Contains(err.Error(), "manifest.json is missing") {
		t.Errorf("expected a missing-manifest error, got %v", err)
	}
}

func TestReadArchiveFile_UnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.tar.gz")
	writeTestArchive(t, path, &Manifest{Version: "2"}, nil, nil)

	_, err := ReadArchiveFile(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported export version") {
		t.Errorf("expected an unsupported-version error, got %v", err)
	}
}

func TestReadArchiveFile_NotGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.tar.gz")
	if err := os.WriteFile(path, []byte("just text"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadArchiveFile(path); err == nil {
		t.Error("expected an error for a file that is not gzip")
	}
}

func TestReadArchiveFile_NoFile(t *testing.T) {
	if _, err := ReadArchiveFile(filepath.Join(t.TempDir(), "absent.tar.gz")); err == nil {
		t.Error("expected an error for a file that does not exist")
	}
}

// --- run (CLI) ---

func TestRun_UsesNamespaceFromManifest(t *testing.T) {
	hash := "aabbcc001122334455667788990011aa"
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "bob.tar.gz")
	writeTestArchive(t, archivePath,
		&Manifest{Source: "bob", Version: "1", Namespace: "bob"},
		map[string]string{hash: "hello #hi"},
		[]map[string]interface{}{nameEntry("bob/greeting", hash)})

	outDir := filepath.Join(dir, "content")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-o", outDir, archivePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, stderr: %s", code, stderr.String())
	}

	if got := readFile(t, filepath.Join(outDir, "greeting.md")); !strings.Contains(got, "hello #hi") {
		t.Errorf("expected rendered content, got:\n%s", got)
	}
	if !strings.Contains(stdout.String(), `"bob"`) {
		t.Errorf("expected the namespace in the success message, got %q", stdout.String())
	}
}

func TestRun_FullExportNeedsNamespaceFlag(t *testing.T) {
	hash := "aabbcc001122334455667788990011aa"
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "all.tar.gz")
	writeTestArchive(t, archivePath,
		&Manifest{Source: "all", Version: "1"}, // full export: no namespace
		map[string]string{hash: "hello"},
		[]map[string]interface{}{nameEntry("bob/greeting", hash)})

	var stdout, stderr bytes.Buffer
	if code := run([]string{archivePath}, &stdout, &stderr); code == 0 {
		t.Fatal("expected failure when the archive has no namespace and none was given")
	}
	if !strings.Contains(stderr.String(), "-namespace") {
		t.Errorf("expected the error to mention -namespace, got %q", stderr.String())
	}

	outDir := filepath.Join(dir, "content")
	stderr.Reset()
	if code := run([]string{"-namespace", "bob", "-o", outDir, archivePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() with -namespace = %d, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "greeting.md")); err != nil {
		t.Errorf("expected greeting.md: %v", err)
	}
}

func TestRun_UsageErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Errorf("no arguments: run() = %d, want 2", code)
	}
	stderr.Reset()
	if code := run([]string{"a.tar.gz", "b.tar.gz"}, &stdout, &stderr); code != 2 {
		t.Errorf("two archives: run() = %d, want 2", code)
	}
	stderr.Reset()
	if code := run([]string{filepath.Join(t.TempDir(), "absent.tar.gz")}, &stdout, &stderr); code != 1 {
		t.Errorf("missing archive: run() = %d, want 1", code)
	}
}
