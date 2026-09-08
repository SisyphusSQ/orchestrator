package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"
)

// 静态目录与实际分发必须同步，防止只添加帮助却漏掉执行入口，或反过来。
func TestCommandCatalogMatchesDispatch(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "cli.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "runCLI" {
			continue
		}
		// 最后一个 switch command 是业务分发，前一个只分类免数据库命令。
		for _, statement := range function.Body.List {
			switchStatement, ok := statement.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			name, ok := switchStatement.Tag.(*ast.Ident)
			if !ok || name.Name != "command" {
				continue
			}
			want = map[string]bool{}
			for _, statement := range switchStatement.Body.List {
				for _, expression := range statement.(*ast.CaseClause).List {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						t.Fatal("command cases must use static names")
					}
					name, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					want[name] = true
				}
			}
		}
	}
	got := map[string]bool{}
	allNames := map[string]bool{}
	for _, command := range commandDefinitions() {
		if got[command.Command] {
			t.Fatalf("duplicate command %q", command.Command)
		}
		got[command.Command] = true
		for _, name := range append([]string{command.Command}, command.Aliases...) {
			if allNames[name] {
				t.Fatalf("ambiguous command or alias %q", name)
			}
			allNames[name] = true
		}
	}
	if len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog and dispatch differ: catalog=%v dispatch=%v", got, want)
	}
}

func TestCommandCatalogIsIndependent(t *testing.T) {
	first := commandDefinitions()
	second := commandDefinitions()
	first[0].Command = "changed"
	for i := range first {
		if len(first[i].Aliases) > 0 {
			first[i].Aliases[0] = "changed-alias"
		}
	}
	if !reflect.DeepEqual(second, commandDefinitions()) {
		t.Fatal("mutating a returned catalog changed another catalog")
	}
}
