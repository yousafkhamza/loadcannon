// Package examples bundles the scenarios/*.json example files directly into
// the loadcannon binary via go:embed. This matters because `install.sh`
// fetches only the compiled binary — someone who installs that way never
// clones the repo, so scenarios/ wouldn't otherwise exist on their machine.
// `loadcannon examples --write <dir>` makes the examples available regardless
// of how loadcannon was installed.
//
// data/*.json here must stay byte-for-byte identical to the root scenarios/
// directory. `make sync-examples` copies root -> here; CI fails the build if
// they've drifted (see .github/workflows/ci.yml).
package examples

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

//go:embed data/*.json
var data embed.FS

// List returns the embedded example filenames, sorted.
func List() ([]string, error) {
	entries, err := fs.ReadDir(data, "data")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Write copies every embedded example into dir (creating it if needed),
// skipping any file that already exists there so it never clobbers edits.
// Returns the paths actually written.
func Write(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	names, err := List()
	if err != nil {
		return nil, err
	}
	var written []string
	for _, name := range names {
		out := filepath.Join(dir, name)
		if _, err := os.Stat(out); err == nil {
			fmt.Fprintf(os.Stderr, "[skip] %s already exists\n", out)
			continue
		}
		b, err := data.ReadFile("data/" + name)
		if err != nil {
			return written, err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return written, err
		}
		written = append(written, out)
	}
	return written, nil
}
