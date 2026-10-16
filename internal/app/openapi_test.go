package app

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformemail "github.com/meherchaitanyabandaru/greenroot-api/platform/email"
	platformsms "github.com/meherchaitanyabandaru/greenroot-api/platform/sms"
	yaml "go.yaml.in/yaml/v3"
)

const specFile = "../../docs/swagger/openapi.yaml"

// Routes that serve the docs themselves are not API operations.
var undocumented = map[string]bool{
	"GET /openapi.yaml": true, "GET /swagger": true, "GET /swagger/index.html": true,
}

func loadSpec(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %v", err)
	}
	return spec
}

func routedOperations(t *testing.T) map[string]bool {
	t.Helper()
	cfg := config.Load()
	r := NewRouter(Dependencies{Config: cfg, JWT: jwtplatform.NewService(cfg.JWT), SMS: platformsms.MockProvider{}, Email: platformemail.MockProvider{}})
	ops := map[string]bool{}
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if route == "" {
			route = "/"
		}
		if key := method + " " + route; !undocumented[key] {
			ops[key] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

// Every registered route must be documented, and every documented
// operation must still exist, so the Swagger UI cannot drift from the code.
func TestOpenAPICoversRouter(t *testing.T) {
	spec := loadSpec(t)
	documented := map[string]bool{}
	for path, item := range spec["paths"].(map[string]any) {
		for method := range item.(map[string]any) {
			switch method {
			case "get", "post", "put", "patch", "delete":
				documented[strings.ToUpper(method)+" "+strings.TrimSuffix(path, "/")] = true
			}
		}
	}
	routed := routedOperations(t)

	var missing, stale []string
	for op := range routed {
		if !documented[op] {
			missing = append(missing, op)
		}
	}
	for op := range documented {
		if !routed[op] {
			stale = append(stale, op)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("%d routes are not in docs/swagger/openapi.yaml:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d documented operations have no route:\n  %s", len(stale), strings.Join(stale, "\n  "))
	}
}

func TestOpenAPIRefsResolve(t *testing.T) {
	raw, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	spec := loadSpec(t)
	components := spec["components"].(map[string]any)
	refRe := regexp.MustCompile(`\$ref:\s*"?#/components/([A-Za-z]+)/([A-Za-z0-9_.-]+)"?`)
	var broken []string
	for _, m := range refRe.FindAllStringSubmatch(string(raw), -1) {
		section, _ := components[m[1]].(map[string]any)
		if _, ok := section[m[2]]; !ok {
			broken = append(broken, m[1]+"/"+m[2])
		}
	}
	if len(broken) > 0 {
		t.Fatalf("unresolved $refs: %v", broken)
	}
}
