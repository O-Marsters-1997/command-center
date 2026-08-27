package agentlog

import (
	"go/build"
	"slices"
	"strings"
	"testing"
)

// TestNoImpureImports is a test because Go cannot fail compilation on an import nobody wrote.
// Stdlib-only, not a named blocklist: a package is Go's unit of dependency, so anything that
// itself execs would smuggle os/exec back in transitively.
func TestNoImpureImports(t *testing.T) {
	t.Parallel()

	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("import dir: %v", err)
	}

	var imports []string
	imports = append(imports, pkg.Imports...)
	imports = append(imports, pkg.TestImports...)
	imports = append(imports, pkg.XTestImports...)

	forbidden := []string{"os/exec", "database/sql", "net/http"}
	for _, path := range imports {
		if slices.Contains(forbidden, path) {
			t.Errorf("internal/agentlog imports %q; this package is pure", path)
		}
		if !isStdlib(path) && path != "github.com/O-Marsters-1997/command-center/internal/agentlog" {
			t.Errorf("internal/agentlog imports non-stdlib %q; only the standard library is allowed", path)
		}
	}
}

// isStdlib reports whether an import path is in the standard library. Every module path has a
// dot in its first element (a domain); no stdlib path does.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
