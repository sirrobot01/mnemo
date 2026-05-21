// Package app contains a layering invariant test. Application services
// (internal/app/*svc) MUST NOT import concrete adapter packages
// (internal/storage/sqlite, internal/storage/postgres) or surface packages
// (internal/api, internal/cli, internal/ui). They may import the storage
// interface package, the domain package, the safety package, and other
// app/*svc packages. This test fails loudly if a future change adds a
// forbidden import. The test skips rather than failing when no service
// packages are present.
package app

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenSubstrings lists import-path fragments that an app/*svc package
// must never depend on directly. Adding to this list is how you tighten the
// boundary; removing requires an architectural decision.
var forbiddenSubstrings = []string{
	"/internal/storage/sqlite",
	"/internal/storage/postgres",
	"/internal/api",
	"/internal/cli",
	"/internal/ui",
}

func TestServicesDoNotImportConcreteAdapters(t *testing.T) {
	servicesRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	entries, err := os.ReadDir(servicesRoot)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), "svc") {
			continue
		}
		dir := filepath.Join(servicesRoot, entry.Name())
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(info os.FileInfo) bool {
			return !strings.HasSuffix(info.Name(), "_test.go")
		}, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for filename, file := range pkg.Files {
				for _, imp := range file.Imports {
					path := strings.Trim(imp.Path.Value, `"`)
					for _, bad := range forbiddenSubstrings {
						if strings.Contains(path, bad) {
							t.Errorf("%s imports forbidden package %q (matches %q)", filename, path, bad)
						}
					}
				}
			}
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no internal/app/*svc packages yet")
	}
}
