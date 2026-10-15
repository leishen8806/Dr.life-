package api_test

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/server"
)

func TestOpenAPIContract(t *testing.T) {
	doc := loadSpec(t)
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantTags := map[string]string{
		"public": "/v1/public", "buyer": "/v1/buyer", "merchant": "/v1/merchant",
		"driver": "/v1/driver", "wms": "/v1/wms", "admin": "/v1/admin",
	}
	for _, tag := range doc.Tags {
		prefix, ok := wantTags[tag.Name]
		if !ok || !strings.Contains(tag.Description, prefix) || tag.Extensions["x-roles"] == nil {
			t.Fatalf("tag %q is missing its path or x-roles contract", tag.Name)
		}
		delete(wantTags, tag.Name)
	}
	if len(wantTags) != 0 {
		t.Fatalf("missing audience tags: %v", wantTags)
	}
	want := sourceErrorCodes(t)
	if got := documentedErrorCodes(t, doc); !equalInts(got, want) {
		t.Fatalf("documented error codes %v do not match httpx constants %v", got, want)
	}
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if _, ok := operation.Extensions["x-roles"]; !ok {
				t.Errorf("%s %s is missing operation-level x-roles", strings.ToUpper(method), path)
			}
		}
	}
}

type matrixAuthenticator struct{}

func (matrixAuthenticator) Authenticate(r *http.Request) (gateway.Identity, bool, error) {
	role := gateway.Role(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return gateway.Identity{AccountID: "matrix-account", Role: role}, true, nil
}

func TestOpenAPIAuthorizationMatrix(t *testing.T) {
	doc := loadSpec(t)
	rule := gateway.Rule{Limit: 1000, Window: time.Minute}
	handler := server.NewHandlerWithGateway(nil, server.GatewayOptions{
		AppEnv: "dev", OriginGroups: gateway.DefaultOriginGroups("dev"), Authenticator: matrixAuthenticator{},
		Limiter: gateway.NewMemoryLimiter(), IPRules: gateway.RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule},
		AccountRules: gateway.RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule},
	})
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			roles := extensionRoles(t, operation.Extensions["x-roles"])
			requestPath := replacePathParameters(path)
			if len(roles) == 1 && roles[0] == "none" {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(strings.ToUpper(method), requestPath, nil))
				if response.Code == http.StatusUnauthorized || response.Code == http.StatusForbidden {
					t.Errorf("public %s %s returned %d", method, path, response.Code)
				}
				continue
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(strings.ToUpper(method), requestPath, nil))
			if response.Code != http.StatusUnauthorized {
				t.Errorf("unauthenticated %s %s returned %d", method, path, response.Code)
			}
			wrong := httptest.NewRequest(strings.ToUpper(method), requestPath, nil)
			wrong.Header.Set("Authorization", "Bearer "+wrongRole(roles))
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, wrong)
			if response.Code != http.StatusForbidden {
				t.Errorf("wrong-role %s %s returned %d", method, path, response.Code)
			}
		}
	}
}

func extensionRoles(t *testing.T, value any) []string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	if err := json.Unmarshal(raw, &roles); err != nil || len(roles) == 0 {
		t.Fatalf("invalid x-roles: %v", value)
	}
	return roles
}

func replacePathParameters(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "test"
		}
	}
	return strings.Join(parts, "/")
}

func wrongRole(roles []string) string {
	for _, candidate := range []string{"buyer", "merchant", "driver", "wms", "admin"} {
		found := false
		for _, allowed := range roles {
			found = found || allowed == candidate
		}
		if !found {
			return candidate
		}
	}
	return "invalid"
}

func TestPublicPingMatchesOpenAPI(t *testing.T) {
	recorder := httptest.NewRecorder()
	server.NewHandler(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	doc := loadSpec(t)
	response := doc.Paths.Value("/v1/public/ping").Get.Responses.Value("200")
	if err := response.Value.Content.Get("application/json").Schema.Value.VisitJSON(body); err != nil {
		t.Fatalf("response does not match OpenAPI schema: %v", err)
	}
}

func TestEveryErrorAndRouterFailureMatchesOpenAPI(t *testing.T) {
	doc := loadSpec(t)
	schema := doc.Components.Schemas["ErrorEnvelope"].Value
	for _, code := range documentedErrorCodes(t, doc) {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			response := httptest.NewRecorder()
			httpx.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.ErrorCode(code)))
			})).ServeHTTP(response, request)
			assertErrorSchema(t, schema, response)
		})
	}

	handler := server.NewHandler(nil)
	for _, test := range []struct {
		name, method, path string
		status             int
		code               httpx.ErrorCode
	}{
		{name: "not found", method: http.MethodGet, path: "/v1/public/missing", status: http.StatusNotFound, code: httpx.CodeResourceNotFound},
		{name: "method not allowed", method: http.MethodOptions, path: "/v1/public/ping", status: http.StatusMethodNotAllowed, code: httpx.CodeMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
			}
			var body struct {
				Error struct {
					Code httpx.ErrorCode `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != test.code {
				t.Fatalf("code=%d want=%d", body.Error.Code, test.code)
			}
			assertErrorSchema(t, schema, response)
		})
	}
}

func assertErrorSchema(t *testing.T, schema *openapi3.Schema, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type=%q", response.Header().Get("Content-Type"))
	}
	var body any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if err := schema.VisitJSON(body); err != nil {
		t.Fatalf("response does not match ErrorEnvelope: %v; body=%s", err, response.Body.String())
	}
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join(filepath.Dir(filename), "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func documentedErrorCodes(t *testing.T, doc *openapi3.T) []int {
	t.Helper()
	values := doc.Components.Schemas["ErrorCode"].Value.Enum
	codes := make([]int, 0, len(values))
	for _, value := range values {
		number, ok := value.(float64)
		if !ok {
			t.Fatalf("error code %v is not a number", value)
		}
		codes = append(codes, int(number))
	}
	sort.Ints(codes)
	return codes
}

func sourceErrorCodes(t *testing.T) []int {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(filename), "..", "platform", "httpx", "response.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var codes []int
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "Code") || strings.HasPrefix(name.Name, "ErrorCode") || i >= len(spec.Values) {
				continue
			}
			literal, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || literal.Kind != token.INT {
				t.Fatalf("%s must have an explicit integer value", name.Name)
			}
			value, err := strconv.Atoi(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			codes = append(codes, value)
		}
		return true
	})
	sort.Ints(codes)
	return codes
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
