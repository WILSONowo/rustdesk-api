package router

import (
	"strings"
	"testing"

	"github.com/lejianwen/rustdesk-api/v2/service"
)

func testClientResources(t *testing.T, request func(string, string, string, any) (int, map[string]any), adminToken, userToken string) {
	t.Helper()
	path := "/api/admin/client-resources"
	for _, method := range []string{"GET", "PUT"} {
		_, res := request(method, path, "", map[string]any{})
		if res["code"] != float64(403) {
			t.Fatalf("anonymous %s allowed: %#v", method, res)
		}
	}
	_, res := request("GET", path, userToken, nil)
	if res["code"] != float64(0) || len(res["data"].(map[string]any)["downloads"].([]any)) != 4 {
		t.Fatalf("missing defaults: %#v", res)
	}
	form := map[string]any{
		"downloads": []map[string]any{
			{"platform": "Windows", "arch": "x64", "version": "1.4.0", "url": "/downloads/client.exe", "enabled": true},
			{"platform": "Linux", "url": "https://downloads.example.test/client.deb", "enabled": false},
		},
		"import_code": "  EXPORTED-CODE+/_=-\nsecond-line  ",
	}
	_, res = request("PUT", path, userToken, form)
	if res["code"] != float64(403) {
		t.Fatalf("ordinary user edited settings: %#v", res)
	}
	_, res = request("PUT", path, adminToken, form)
	if res["code"] != float64(0) {
		t.Fatalf("save failed: %#v", res)
	}
	wantCode := "EXPORTED-CODE+/_=-\nsecond-line"
	_, res = request("GET", path, userToken, nil)
	data := res["data"].(map[string]any)
	if data["import_code"] != wantCode || len(data["downloads"].([]any)) != 1 {
		t.Fatalf("wrong user resources: %#v", res)
	}
	_, res = request("GET", path, adminToken, nil)
	if len(res["data"].(map[string]any)["downloads"].([]any)) != 2 {
		t.Fatal("admin cannot edit hidden downloads")
	}
	// Re-read from the database, not a cached response.
	persisted, err := service.LoadClientResources()
	if err != nil || persisted.ImportCode != wantCode || len(persisted.Downloads) != 2 {
		t.Fatalf("settings not persisted: %v", err)
	}
	for _, link := range []string{"javascript:alert(1)", "data:text/html,test", "//evil.example/file", "/\\evil.example/file", "/%2f%2fevil.example/file", "https://user:pass@example.test/file", "https://example.test/a\nb", "ftp://example.test/file"} {
		_, res = request("PUT", path, adminToken, map[string]any{"downloads": []map[string]any{{"platform": "Windows", "url": link, "enabled": true}}})
		if res["code"] != float64(101) {
			t.Fatalf("unsafe URL accepted %q: %#v", link, res)
		}
	}
	_, res = request("PUT", path, adminToken, map[string]any{"import_code": strings.Repeat("a", 16385)})
	if res["code"] != float64(101) {
		t.Fatal("oversize import code accepted")
	}
	_, res = request("PUT", path, adminToken, map[string]any{"downloads": []map[string]any{{"platform": "unknown", "url": "https://example.test"}}})
	if res["code"] != float64(101) {
		t.Fatal("unknown platform accepted")
	}
	persisted, err = service.LoadClientResources()
	if err != nil || persisted.ImportCode != wantCode || len(persisted.Downloads) != 2 {
		t.Fatal("invalid write modified settings")
	}
	_, res = request("PUT", path, adminToken, map[string]any{"downloads": []any{}, "import_code": ""})
	if res["code"] != float64(0) {
		t.Fatal("clearing resources failed")
	}
	_, res = request("GET", path, userToken, nil)
	data = res["data"].(map[string]any)
	if data["import_code"] != "" || len(data["downloads"].([]any)) != 0 {
		t.Fatal("clear not persisted")
	}
}
