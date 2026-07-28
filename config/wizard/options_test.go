package wizard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestLegacyOptionCompatibilityAPIIsRemoved(t *testing.T) {
	legacyNames := map[string]struct{}{
		"RunOptions":            {},
		"InstallOptions":        {},
		"DefaultRunOptions":     {},
		"DefaultInstallOptions": {},
	}

	for _, filename := range []string{"options.go", "interactive.go", "install.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}

		ast.Inspect(file, func(node ast.Node) bool {
			var name string
			switch declaration := node.(type) {
			case *ast.FuncDecl:
				name = declaration.Name.Name
			case *ast.TypeSpec:
				name = declaration.Name.Name
			}
			if _, found := legacyNames[name]; found {
				t.Errorf("legacy compatibility API %s still exists in %s", name, filename)
			}
			return true
		})
	}
}
