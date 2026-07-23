package cinemeta_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestModuleImportsOnlyPublishedContracts is the module boundary made
// executable: this module must use only the published SDK and the standard
// library. It is a separate Go module, so Go itself already rejects a
// Platform-internal import; this parse keeps the intent explicit and catches a
// third-party dependency creeping in too (ADR 0008, ADR 0016).
//
// It matters here for a reason particular to a core module. This one is
// compiled into the Platform binary and shares its dependency graph
// (ADR 0062), so a dependency added here is a dependency the Platform and every
// other core module must resolve at a compatible version. The boundary is what
// keeps the tier a *delivery* decision rather than a contract one: this module
// is written exactly as a third party's would be, and could move out of process
// as a build change rather than a rewrite (ADR 0064).
//
// Note the absence of an `sdui` exemption. The Stremio module has one, because
// it contributes a settings screen (ADR 0038); this module has no settings, so
// it has no reason to reach the UI contract at all.
func TestModuleImportsOnlyPublishedContracts(t *testing.T) {
	const (
		sdkPrefix      = "github.com/mosaic-media/sdk/"
		platformPrefix = "github.com/mosaic-media/platform/"
	)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++

		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			switch {
			// Standard-library imports have no dot in their first segment.
			case !strings.Contains(strings.SplitN(path, "/", 2)[0], "."):
			case strings.HasPrefix(path, sdkPrefix):
				// The published SDK — the contract this module is built against.
			case strings.HasPrefix(path, platformPrefix):
				t.Errorf("%s imports private Platform package %q; a module may import only the SDK", name, path)
			default:
				t.Errorf("%s imports third-party package %q; this module may use only the SDK and the standard library", name, path)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no non-test source files were checked; the boundary test is not looking at anything")
	}
}
