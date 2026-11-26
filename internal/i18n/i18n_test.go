package i18n

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestLanguageAndFallback(t *testing.T) {
	defer SetLanguage("zh-CN")
	SetLanguage("zh-CN")
	if T("主密码错误") != "主密码错误" {
		t.Fatal("Chinese should be the default")
	}
	SetLanguage("en")
	if T("主密码错误") != "Incorrect master password" {
		t.Fatal("missing English translation")
	}
	if T("user supplied text") != "user supplied text" {
		t.Fatal("unknown text must be preserved")
	}
	SetLanguage("invalid")
	if T("主密码错误") != "主密码错误" {
		t.Fatal("unsupported language must fall back to Chinese")
	}
}

func TestFormatPlaceholders(t *testing.T) {
	pattern := regexp.MustCompile(`%[+#\- 0]*[0-9]*\.?[0-9]*[a-zA-Z]`)
	for source, translated := range english {
		a, b := pattern.FindAllString(source, -1), pattern.FindAllString(translated, -1)
		if !slices.Equal(a, b) {
			t.Errorf("format changed for %q: %v -> %v", source, a, b)
		}
	}
}

func TestSentinelLanguageChangesPreserveIdentity(t *testing.T) {
	defer SetLanguage("zh-CN")
	SetLanguage("zh-CN")
	sentinel := NewError("主密码错误")
	if sentinel.Error() != "主密码错误" {
		t.Fatal("sentinel did not use Chinese")
	}
	SetLanguage("en")
	if sentinel.Error() != "Incorrect master password" {
		t.Fatal("sentinel retained its initialization language")
	}
	if !errors.Is(fmt.Errorf("operation failed: %w", sentinel), sentinel) {
		t.Fatal("localization changed sentinel identity")
	}
}

func TestCatalogCoversBackendTranslationCalls(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "T" && selector.Sel.Name != "NewError") {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "i18n" {
				return true
			}
			if message, ok := translationLiteral(call.Args[0]); ok {
				if _, found := english[message]; !found {
					t.Errorf("%s: missing English translation for %q", fset.Position(call.Pos()), message)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func translationLiteral(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			text, err := strconv.Unquote(value.Value)
			return text, err == nil
		}
	case *ast.BinaryExpr:
		if value.Op == token.ADD {
			left, a := translationLiteral(value.X)
			right, b := translationLiteral(value.Y)
			return left + right, a && b
		}
	}
	return "", false
}
