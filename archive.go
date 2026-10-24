package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// supportedManifestVersion is the only Hatcheck export format this tool
// understands. An archive declaring any other version is refused rather
// than guessed at.
const supportedManifestVersion = "1"

// Manifest mirrors the manifest.json at the root of a Hatcheck export
// archive. Only the fields this tool needs are meaningful here; the rest
// are carried so a caller can report on them.
type Manifest struct {
	Source    string    `json:"source"`
	Exported  time.Time `json:"exported"`
	Version   string    `json:"version"`
	Objects   int       `json:"objects"`
	Name      string    `json:"name,omitempty"`      // set for single-name partial exports
	Namespace string    `json:"namespace,omitempty"` // set for namespace-scoped partial exports
}

// Archive is a Hatcheck export read fully into memory: the manifest, every
// CAS object keyed by its hash, and the metadata log entries.
//
// A Hatcheck export is a tar.gz with this layout:
//
//	manifest.json
//	objects/<first two hex chars>/<remaining hex chars>   one per CAS object
//	metadata/log.json                                      JSON array of log entries
//
// Objects are held in memory because the tar stream is sequential and the
// log (which says which objects matter) comes after them. A namespace-scoped
// export is normally small; a full export of a very large store would need
// a streaming approach this tool does not have.
type Archive struct {
	Manifest Manifest
	Objects  map[string][]byte
	Log      []json.RawMessage
}

// ReadArchiveFile opens and reads the archive at path.
func ReadArchiveFile(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	a, err := ReadArchive(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return a, nil
}

// ReadArchive reads a Hatcheck export archive from r. Entries that are not
// part of the export format are ignored, so a newer exporter adding files
// does not break an older reader (the manifest version guards real format
// changes).
func ReadArchive(r io.Reader) (*Archive, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("not a gzip file: %w", err)
	}
	defer gz.Close()

	a := &Archive{Objects: make(map[string][]byte)}
	haveManifest := false
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		switch {
		case hdr.Name == "manifest.json":
			if err := json.NewDecoder(tr).Decode(&a.Manifest); err != nil {
				return nil, fmt.Errorf("decoding manifest.json: %w", err)
			}
			haveManifest = true

		case strings.HasPrefix(hdr.Name, "objects/"):
			// objects/ab/cdef... -> hash "abcdef..."
			hash := strings.ReplaceAll(strings.TrimPrefix(hdr.Name, "objects/"), "/", "")
			if hash == "" {
				continue
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading object %s: %w", hash, err)
			}
			a.Objects[hash] = data

		case hdr.Name == "metadata/log.json":
			if err := json.NewDecoder(tr).Decode(&a.Log); err != nil {
				return nil, fmt.Errorf("decoding metadata/log.json: %w", err)
			}
		}
	}

	if !haveManifest {
		return nil, fmt.Errorf("not a Hatcheck export: manifest.json is missing")
	}
	if a.Manifest.Version != supportedManifestVersion {
		return nil, fmt.Errorf("unsupported export version %q (this tool reads version %q)",
			a.Manifest.Version, supportedManifestVersion)
	}
	return a, nil
}

// Object returns the content of the CAS object with the given hash.
func (a *Archive) Object(hash string) (string, error) {
	data, ok := a.Objects[hash]
	if !ok {
		return "", fmt.Errorf("object %s is not in the archive", hash)
	}
	return string(data), nil
}
