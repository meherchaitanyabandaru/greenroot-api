// Command sqlcheck validates every complete SQL statement written as a Go
// string constant against a real database schema, without running it:
// each statement is PREPAREd (parsed, name-resolved and type-checked) but
// never executed, so it is safe against any database.
//
//	DATABASE_URL=... go run ./cmd/sqlcheck ./internal/...
//
// A statement is a string literal (or a constant concatenation of
// literals) whose text starts with SELECT, INSERT, UPDATE, DELETE or WITH.
// Fragments assembled at run time (WHERE clauses, ORDER BY allowlists) are
// out of scope; they are covered by the integration tests. Exit status 1
// lists every statement PostgreSQL rejects, with file:line.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type statement struct {
	pos token.Position
	sql string
}

// SQL in this codebase is upper-case; mixed-case text ("Update Trip") is UI copy.
var leading = regexp.MustCompile(`(?s)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH)\b`)

func main() {
	verbose := flag.Bool("v", false, "list every statement checked")
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"./internal/..."}
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "sqlcheck: DATABASE_URL is required")
		os.Exit(2)
	}
	stmts, err := collect(roots)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sqlcheck:", err)
		os.Exit(2)
	}
	failures, skipped, err := check(url, stmts, *verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sqlcheck:", err)
		os.Exit(2)
	}
	for _, f := range failures {
		fmt.Println(f)
	}
	fmt.Printf("sqlcheck: %d statements, %d rejected, %d not checkable (parameter types unresolved)\n",
		len(stmts), len(failures), skipped)
	if len(failures) > 0 {
		os.Exit(1)
	}
}

// collect walks Go files (excluding tests) and returns SQL statements.
func collect(roots []string) ([]statement, error) {
	var files []string
	for _, root := range roots {
		dir := strings.TrimSuffix(root, "/...")
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && (info.Name() == "testdata" || info.Name() == "vendor") {
				return filepath.SkipDir
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)

	fset := token.NewFileSet()
	var out []statement
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		// Literals that are only part of a statement: operands of a
		// non-constant concatenation, and fmt-style templates.
		fragment := map[ast.Node]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.BinaryExpr:
				if _, ok := constString(e); !ok && e.Op == token.ADD {
					markOperands(e, fragment)
				}
			case *ast.CallExpr:
				if sel, ok := e.Fun.(*ast.SelectorExpr); ok && strings.HasSuffix(sel.Sel.Name, "printf") || isSprintf(e) {
					for _, arg := range e.Args {
						fragment[arg] = true
					}
				}
			case *ast.AssignStmt:
				// q += " AND ..." builds a statement at run time.
				if e.Tok == token.ADD_ASSIGN {
					for _, rhs := range e.Rhs {
						fragment[rhs] = true
					}
					for _, lhs := range e.Lhs {
						markIdent(lhs, f, fragment)
					}
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			if fragment[n] {
				return false
			}
			switch e := n.(type) {
			case *ast.BinaryExpr:
				// A constant "a" + "b" concatenation is one statement.
				if s, ok := constString(e); ok {
					if leading.MatchString(s) && complete(s) {
						out = append(out, statement{fset.Position(e.Pos()), s})
					}
					return false
				}
			case *ast.BasicLit:
				if s, ok := constString(e); ok && leading.MatchString(s) && complete(s) {
					out = append(out, statement{fset.Position(e.Pos()), s})
				}
			}
			return true
		})
	}
	return out, nil
}

// markOperands flags the literal operands of a run-time concatenation.
func markOperands(e ast.Expr, frag map[ast.Node]bool) {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			markOperands(v.X, frag)
			markOperands(v.Y, frag)
		}
	case *ast.ParenExpr:
		markOperands(v.X, frag)
	case *ast.BasicLit:
		frag[v] = true
	case *ast.Ident:
		// A named constant used as a prefix: flag its declaration value.
		if _, ok := constString(v); ok {
			return
		}
		if v.Obj != nil {
			if spec, ok := v.Obj.Decl.(*ast.ValueSpec); ok {
				for _, val := range spec.Values {
					frag[val] = true
				}
			}
		}
	}
}

// markIdent flags the initial value of a variable that is later extended
// with +=, since the statement is only complete at run time.
func markIdent(e ast.Expr, f *ast.File, frag map[ast.Node]bool) {
	id, ok := e.(*ast.Ident)
	if !ok || id.Obj == nil {
		return
	}
	switch d := id.Obj.Decl.(type) {
	case *ast.AssignStmt:
		for _, rhs := range d.Rhs {
			frag[rhs] = true
		}
	case *ast.ValueSpec:
		for _, v := range d.Values {
			frag[v] = true
		}
	}
}

func isSprintf(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}

// complete rejects bare keywords such as the audit action "UPDATE" and
// texts that still carry fmt verbs.
func complete(s string) bool {
	if strings.Contains(s, "%") && regexp.MustCompile(`%[dsvq]`).MatchString(s) {
		return false
	}
	return len(strings.Fields(s)) > 1
}

func constString(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok1 := constString(v.X)
		r, ok2 := constString(v.Y)
		return l + r, ok1 && ok2
	case *ast.ParenExpr:
		return constString(v.X)
	case *ast.Ident:
		// A named string constant (const cols = "a, b") is known at compile time.
		if v.Obj != nil && v.Obj.Kind == ast.Con {
			if spec, ok := v.Obj.Decl.(*ast.ValueSpec); ok {
				for i, name := range spec.Names {
					if name.Name == v.Name && i < len(spec.Values) {
						return constString(spec.Values[i])
					}
				}
			}
		}
	}
	return "", false
}

// check PREPAREs each statement. "could not determine data type of
// parameter" means the statement is valid SQL whose placeholders only get
// a type from the Go argument; it is counted, not failed.
func check(url string, stmts []statement, verbose bool) ([]string, int, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = db.Close() }() // CLI exit: nothing left to flush
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var failures []string
	skipped := 0
	for i, st := range stmts {
		conn, err := db.Conn(ctx)
		if err != nil {
			return nil, 0, err
		}
		_, err = conn.ExecContext(ctx, fmt.Sprintf("PREPARE sqlcheck_%d AS %s", i, st.sql))
		_ = conn.Close()
		if verbose {
			fmt.Printf("%s: %v\n", st.pos, err)
		}
		if err == nil {
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P18" { // indeterminate_datatype
			skipped++
			continue
		}
		failures = append(failures, fmt.Sprintf("%s: %v\n    %s", st.pos, err, oneLine(st.sql)))
	}
	return failures, skipped, nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
