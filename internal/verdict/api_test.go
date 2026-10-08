package verdict

import (
	"go/build"
	"slices"
	"strings"
	"testing"
)

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
			t.Errorf("internal/verdict imports %q; this package is pure", path)
		}
		if !isStdlib(path) && path != "github.com/O-Marsters-1997/command-center/internal/verdict" {
			t.Errorf("internal/verdict imports non-stdlib %q; only the standard library is allowed", path)
		}
	}
}

func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
