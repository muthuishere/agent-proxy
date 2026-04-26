package acceptance_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestMaskBase64WithEqualSignPrefix verifies that a base64-encoded secret preceded
// by a key=value '=' is correctly detected (regression for the = boundary bug).
func TestMaskBase64WithEqualSignPrefix(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()

	rawConn := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	b64Conn := base64.StdEncoding.EncodeToString([]byte(rawConn))

	bodyObj := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "base64_database_url=" + b64Conn,
			},
		},
	}
	bodyBytes, _ := json.Marshal(bodyObj)
	headers := http.Header{"Content-Type": []string{"application/json"}}

	out, count, err := svc.HandleRequest("b64boundary", "api.anthropic.com", "/v1/messages", "POST", headers, bodyBytes, "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected base64 blob to be masked, got 0\nbody: %s", string(out))
	}
	if strings.Contains(string(out), rawConn) {
		t.Fatalf("raw connection string still present after masking")
	}
	if strings.Contains(string(out), b64Conn) {
		t.Fatalf("base64 blob still present after masking (= boundary bug not fixed)")
	}
	assertValidJSON(t, string(out))
}
