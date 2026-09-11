package repository_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/openark/orchestrator"

func projectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate architecture test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func productionGoFiles(t *testing.T) []string {
	t.Helper()
	root := projectRoot(t)
	var files []string
	for _, directory := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", directory, err)
		}
	}
	sort.Strings(files)
	return files
}

func allProjectGoFiles(t *testing.T) []string {
	t.Helper()
	root := projectRoot(t)
	var files []string
	for _, directory := range []string{"cmd", "docs", "internal", "tests", "tools", "web"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", directory, err)
		}
	}
	sort.Strings(files)
	return files
}

func importsOf(t *testing.T, filename string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	imports := make([]string, 0, len(parsed.Imports))
	for _, declaration := range parsed.Imports {
		path, err := strconv.Unquote(declaration.Path.Value)
		if err != nil {
			t.Fatalf("decode import in %s: %v", filename, err)
		}
		imports = append(imports, path)
	}
	return imports
}

func TestDatabaseDependenciesStayInRepository(t *testing.T) {
	root := projectRoot(t)
	var violations []string
	for _, filename := range productionGoFiles(t) {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(filepath.ToSlash(relative), "internal/repository/") {
			continue
		}
		modelsMayUseSQLScalars := strings.HasPrefix(filepath.ToSlash(relative), "internal/models/do/") ||
			strings.HasPrefix(filepath.ToSlash(relative), "internal/models/domain/")
		for _, imported := range importsOf(t, filename) {
			if imported == modulePath+"/internal/repository/database" ||
				(imported == "database/sql" && !modelsMayUseSQLScalars) ||
				imported == "github.com/go-sql-driver/mysql" ||
				imported == "github.com/mattn/go-sqlite3" ||
				strings.HasPrefix(imported, "gorm.io/") {
				violations = append(violations, fmt.Sprintf("%s imports %s", relative, imported))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("database implementation dependencies escaped internal/repository:\n%s", strings.Join(violations, "\n"))
	}
}

func TestRepositoryDoesNotDependOnBusinessPackages(t *testing.T) {
	root := projectRoot(t)
	forbidden := []string{
		modulePath + "/internal/agent",
		modulePath + "/internal/app",
		modulePath + "/internal/http",
		modulePath + "/internal/inst",
		modulePath + "/internal/logic",
		modulePath + "/internal/process",
		modulePath + "/internal/recoverypolicy",
		modulePath + "/internal/models/dto",
		modulePath + "/internal/models/vo",
	}
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal", "repository"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, imported := range importsOf(t, path) {
			for _, prefix := range forbidden {
				if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
					violations = append(violations, fmt.Sprintf("%s imports %s", relative, imported))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("repository depends on business packages:\n%s", strings.Join(violations, "\n"))
	}
}

func TestDAOFilesStayInRepository(t *testing.T) {
	root := projectRoot(t)
	var violations []string
	for _, filename := range productionGoFiles(t) {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			t.Fatal(err)
		}
		relative = filepath.ToSlash(relative)
		if !strings.HasPrefix(relative, "internal/repository/") && strings.HasSuffix(relative, "_dao.go") {
			violations = append(violations, relative)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("legacy DAO files found outside internal/repository:\n%s", strings.Join(violations, "\n"))
	}
}

func TestPersistenceModelsStayInModelsDOOrRepository(t *testing.T) {
	root := projectRoot(t)
	var violations []string
	for _, filename := range productionGoFiles(t) {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			t.Fatal(err)
		}
		relative = filepath.ToSlash(relative)
		if strings.HasPrefix(relative, "internal/models/do/") || strings.HasPrefix(relative, "internal/repository/") {
			continue
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		if strings.Contains(string(contents), `gorm:"column:`) {
			violations = append(violations, relative)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("persistence model definitions escaped internal/models/do:\n%s", strings.Join(violations, "\n"))
	}
}

func TestPersistenceModelsDoNotEscapeRepository(t *testing.T) {
	root := projectRoot(t)
	var violations []string
	for _, filename := range allProjectGoFiles(t) {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			t.Fatal(err)
		}
		relative = filepath.ToSlash(relative)
		if strings.HasPrefix(relative, "internal/models/do/") || strings.HasPrefix(relative, "internal/repository/") {
			continue
		}
		for _, imported := range importsOf(t, filename) {
			if imported == modulePath+"/internal/models/do" || strings.HasPrefix(imported, modulePath+"/internal/models/do/") {
				violations = append(violations, fmt.Sprintf("%s imports %s", relative, imported))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("persistence models escaped repository boundaries:\n%s", strings.Join(violations, "\n"))
	}
}

func TestRepositoryPublicAPIsDoNotExposePersistenceModels(t *testing.T) {
	root := projectRoot(t)
	repositoryRoot := filepath.Join(root, "internal", "repository")
	fileSet := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(repositoryRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		persistenceImports := make(map[string]struct{})
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if importPath != modulePath+"/internal/models/do" && !strings.HasPrefix(importPath, modulePath+"/internal/models/do/") {
				continue
			}
			importName := filepath.Base(importPath)
			if imported.Name != nil {
				importName = imported.Name.Name
			}
			if importName == "." {
				relative, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return relErr
				}
				violations = append(violations, fmt.Sprintf("%s dot-imports persistence models", filepath.ToSlash(relative)))
				continue
			}
			persistenceImports[importName] = struct{}{}
		}
		if len(persistenceImports) == 0 {
			return nil
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !ast.IsExported(function.Name.Name) {
				continue
			}
			exposesPersistence := false
			ast.Inspect(function.Type, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				if _, ok := persistenceImports[identifier.Name]; ok {
					exposesPersistence = true
					return false
				}
				return true
			})
			if exposesPersistence {
				position := fileSet.Position(function.Pos())
				relative, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return relErr
				}
				violations = append(violations, fmt.Sprintf("%s:%d exposes persistence models from %s", filepath.ToSlash(relative), position.Line, function.Name.Name))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("repository public APIs expose persistence models:\n%s", strings.Join(violations, "\n"))
	}
}

func TestMetadataPublicAPIsDoNotAcceptRawQueryFragments(t *testing.T) {
	root := projectRoot(t)
	metadataRoot := filepath.Join(root, "internal", "repository", "metadata")
	fileSet := token.NewFileSet()
	forbiddenParameterNames := map[string]struct{}{
		"clause":    {},
		"condition": {},
		"orderBy":   {},
		"query":     {},
		"sort":      {},
		"statement": {},
	}
	var violations []string
	err := filepath.WalkDir(metadataRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !ast.IsExported(function.Name.Name) || function.Type.Params == nil {
				continue
			}
			unsafeParameter := ""
			for _, field := range function.Type.Params.List {
				if ellipsis, ok := field.Type.(*ast.Ellipsis); ok {
					switch element := ellipsis.Elt.(type) {
					case *ast.Ident:
						if element.Name == "any" || element.Name == "string" {
							unsafeParameter = "raw variadic arguments"
						}
					case *ast.InterfaceType:
						if element.Methods == nil || len(element.Methods.List) == 0 {
							unsafeParameter = "raw variadic arguments"
						}
					}
					if unsafeParameter != "" {
						break
					}
				}
				parameterType, isString := field.Type.(*ast.Ident)
				if !isString || parameterType.Name != "string" {
					continue
				}
				for _, name := range field.Names {
					if _, ok := forbiddenParameterNames[name.Name]; ok {
						unsafeParameter = name.Name
						break
					}
				}
				if unsafeParameter != "" {
					break
				}
			}
			if unsafeParameter == "" {
				continue
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			position := fileSet.Position(function.Pos())
			violations = append(violations, fmt.Sprintf(
				"%s:%d exposes %s from %s",
				filepath.ToSlash(relative), position.Line, unsafeParameter, function.Name.Name,
			))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("metadata public APIs accept raw query fragments:\n%s", strings.Join(violations, "\n"))
	}
}

func TestTransportModelsDoNotDependOnPersistence(t *testing.T) {
	root := projectRoot(t)
	var violations []string
	for _, directory := range []string{"domain", "dto", "vo"} {
		err := filepath.WalkDir(filepath.Join(root, "internal", "models", directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			for _, imported := range importsOf(t, path) {
				if imported == modulePath+"/internal/repository" || strings.HasPrefix(imported, modulePath+"/internal/repository/") ||
					imported == modulePath+"/internal/models/do" || strings.HasPrefix(imported, modulePath+"/internal/models/do/") ||
					strings.HasPrefix(imported, "gorm.io/") {
					violations = append(violations, fmt.Sprintf("%s imports %s", relative, imported))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("domain/transport models depend on persistence:\n%s", strings.Join(violations, "\n"))
	}
}

func TestHTTPDoesNotDependOnPersistenceModels(t *testing.T) {
	root := projectRoot(t)
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal", "http"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, imported := range importsOf(t, path) {
			if imported == modulePath+"/internal/models/do" || strings.HasPrefix(imported, modulePath+"/internal/models/do/") {
				violations = append(violations, fmt.Sprintf("%s imports %s", relative, imported))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("HTTP depends on persistence models:\n%s", strings.Join(violations, "\n"))
	}
}

func TestLargePackageSubpackageBoundaries(t *testing.T) {
	root := projectRoot(t)
	for _, namespace := range []string{"internal/inst", "internal/inst/change", "internal/logic"} {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(namespace)))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
				t.Fatalf("%s must only be a namespace; found %s", namespace, entry.Name())
			}
		}
	}

	httpRoot := filepath.Join(root, "internal", "http")
	allowedHTTPRootFiles := map[string]bool{
		"action_guard.go": true,
		"routes.go":       true,
	}
	entries, err := os.ReadDir(httpRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		if !allowedHTTPRootFiles[entry.Name()] {
			t.Fatalf("internal/http root must only compose routes; found %s", entry.Name())
		}
	}

	var httpRootImportViolations []string
	err = filepath.WalkDir(httpRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Dir(path) == httpRoot {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, imported := range importsOf(t, path) {
			if imported == modulePath+"/internal/http" {
				httpRootImportViolations = append(httpRootImportViolations, fmt.Sprintf("%s imports %s", filepath.ToSlash(relative), imported))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(httpRootImportViolations) > 0 {
		t.Fatalf("HTTP subpackages depend on the root composition package:\n%s", strings.Join(httpRootImportViolations, "\n"))
	}

	checks := []struct {
		directory string
		forbidden []string
	}{
		{
			directory: "internal/http/transport",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/http",
				modulePath + "/internal/inst",
				modulePath + "/internal/logic",
				modulePath + "/internal/repository",
			},
		},
		{
			directory: "internal/inst",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/http",
				modulePath + "/internal/logic",
			},
		},
		{
			directory: "internal/inst/analysis",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/http",
				modulePath + "/internal/logic",
			},
		},
		{
			directory: "internal/inst/inventory",
			forbidden: []string{
				modulePath + "/internal/inst/change",
				modulePath + "/internal/inst/discovery",
			},
		},
		{
			directory: "internal/inst/change/replication",
			forbidden: []string{
				modulePath + "/internal/inst/change/regroup",
				modulePath + "/internal/inst/change/relocation",
			},
		},
		{
			directory: "internal/inst/change/relocation",
			forbidden: []string{
				modulePath + "/internal/inst/change/regroup",
			},
		},
		{
			directory: "internal/inst/topology",
			forbidden: []string{
				modulePath + "/internal/inst/change",
			},
		},
		{
			directory: "internal/logic/recovery",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/http",
				modulePath + "/internal/logic/discovery",
				modulePath + "/internal/logic/raftstate",
			},
		},
		{
			directory: "internal/logic/discovery",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/http",
				modulePath + "/internal/logic/raftstate",
			},
		},
		{
			directory: "internal/logic/raftstate",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/http",
			},
		},
	}

	var violations []string
	for _, check := range checks {
		directory := filepath.Join(root, filepath.FromSlash(check.directory))
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			for _, imported := range importsOf(t, path) {
				for _, prefix := range check.forbidden {
					if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
						violations = append(violations, fmt.Sprintf("%s imports %s", filepath.ToSlash(relative), imported))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, directory := range []string{"internal/inst/gtid", "internal/inst/mysql"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(directory)), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			for _, imported := range importsOf(t, path) {
				if strings.HasPrefix(imported, modulePath+"/internal/") {
					violations = append(violations, fmt.Sprintf("%s imports %s", filepath.ToSlash(relative), imported))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("large-package subpackage boundaries were violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestDomainModelsStayIndependent(t *testing.T) {
	root := projectRoot(t)
	forbidden := []string{
		modulePath + "/internal/config",
		modulePath + "/internal/http",
		modulePath + "/internal/repository",
		"gorm.io/",
	}
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal", "models", "domain"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, imported := range importsOf(t, path) {
			for _, prefix := range forbidden {
				if imported == prefix || strings.HasPrefix(imported, prefix) {
					violations = append(violations, fmt.Sprintf("%s imports %s", relative, imported))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("domain models depend on infrastructure or transport:\n%s", strings.Join(violations, "\n"))
	}
}

func TestProjectContainsNoTypeAliases(t *testing.T) {
	root := projectRoot(t)
	fileSet := token.NewFileSet()
	var violations []string
	for _, filename := range allProjectGoFiles(t) {
		parsed, err := parser.ParseFile(fileSet, filename, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			typeSpec, ok := node.(*ast.TypeSpec)
			if !ok || !typeSpec.Assign.IsValid() {
				return true
			}
			position := fileSet.Position(typeSpec.Pos())
			relative, relErr := filepath.Rel(root, filename)
			if relErr != nil {
				t.Fatalf("relative path for %s: %v", filename, relErr)
			}
			violations = append(violations, fmt.Sprintf("%s:%d aliases %s", filepath.ToSlash(relative), position.Line, typeSpec.Name.Name))
			return true
		})
	}
	if len(violations) > 0 {
		t.Fatalf("type aliases are not allowed; depend on the canonical model directly:\n%s", strings.Join(violations, "\n"))
	}
}
