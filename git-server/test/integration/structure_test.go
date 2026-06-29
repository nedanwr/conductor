package integration

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// repoRoot returns the git-server module root, derived from this test file's
// location so the structural checks run regardless of the working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// internalGoFiles walks internal/ and returns every non-generated, non-test Go
// source file: the application code the architectural seams constrain.
func internalGoFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal")
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Generated protobuf/Connect stubs are not application code.
		if strings.Contains(path, string(filepath.Separator)+"gen"+string(filepath.Separator)) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	return files
}

// rel renders a path relative to the module root for readable failures.
func rel(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.Rel(repoRoot(t), path)
	if err != nil {
		return path
	}
	return r
}

// TestModeAwarenessDoesNotLeak asserts that runtime role is interpreted in
// exactly one place. The wiring root under internal/app is the only package
// allowed to name a Mode; if "Mode" appears in application code anywhere below
// it, mode-branching has leaked into a service and location transparency is
// broken. This is the no-mode-branching standing check, enforced as a test.
func TestModeAwarenessDoesNotLeak(t *testing.T) {
	appDir := string(filepath.Separator) + filepath.Join("internal", "app") + string(filepath.Separator)
	for _, path := range internalGoFiles(t) {
		if strings.Contains(path, appDir) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "Mode") {
				t.Errorf("%s:%d references Mode outside internal/app: %s",
					rel(t, path), i+1, strings.TrimSpace(line))
			}
		}
	}
}

// TestNoServiceImportsPeerImpl asserts every cross-service call crosses through a
// core interface rather than a concrete peer. A service package (or any caller
// other than the wiring root) that imports another service's implementation
// package — repostorage, registry, or cache — has bypassed the seam; legitimate
// cross-service references go through internal/core and the generated Connect
// clients. Only internal/app, which assembles them, may import the impls.
func TestNoServiceImportsPeerImpl(t *testing.T) {
	const modulePrefix = "github.com/nedanwr/conductor/git-server/internal/"
	impls := map[string]bool{"repostorage": true, "registry": true, "cache": true}

	fset := token.NewFileSet()
	for _, path := range internalGoFiles(t) {
		r := rel(t, path)
		// The wiring root and the impls themselves are allowed these imports.
		segs := strings.Split(filepath.ToSlash(r), "/")
		owning := segs[1] // internal/<owning>/...
		if owning == "app" || impls[owning] {
			continue
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil || !strings.HasPrefix(p, modulePrefix) {
				continue
			}
			pkg := strings.TrimPrefix(p, modulePrefix)
			top := strings.SplitN(pkg, "/", 2)[0]
			if impls[top] {
				t.Errorf("%s imports peer implementation %q; cross-service calls must go through internal/core + generated clients", r, p)
			}
		}
	}
}
