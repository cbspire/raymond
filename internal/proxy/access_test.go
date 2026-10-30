package proxy

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogRecordsResponseWithoutRequestSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	h := accessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "response-secret"})
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}), logger)
	r := httptest.NewRequest("POST", "http://site.raymond.test/wp-admin/?token=query-secret", strings.NewReader("body-secret"))
	r.Header.Set("Authorization", "Bearer header-secret")
	r.Header.Set("Cookie", "session=cookie-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var event map[string]any
	if e := json.Unmarshal(output.Bytes(), &event); e != nil {
		t.Fatal(e)
	}
	if event["method"] != "POST" || event["host"] != "site.raymond.test" || event["path"] != "/wp-admin/" || event["status"] != float64(201) || event["bytes"] != float64(2) {
		t.Fatalf("unexpected access log: %v", event)
	}
	if _, ok := event["duration_ms"]; !ok {
		t.Fatal("missing request timing")
	}
	for _, secret := range []string{"query-secret", "body-secret", "header-secret", "cookie-secret", "response-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("request secret leaked: %s", secret)
		}
	}
	if w.Code != 201 || w.Body.String() != "ok" || !strings.Contains(w.Header().Get("Set-Cookie"), "response-secret") {
		t.Fatal("logging changed the response")
	}
}

func TestAccessLogHandlesInformationalAndEmptyResponses(t *testing.T) {
	for _, info := range []bool{false, true} {
		var output bytes.Buffer
		h := accessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if info {
				w.WriteHeader(http.StatusEarlyHints)
				w.WriteHeader(http.StatusNoContent)
			}
		}), slog.New(slog.NewJSONHandler(&output, nil)))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://raymond.test/", nil))
		var event map[string]any
		if e := json.Unmarshal(output.Bytes(), &event); e != nil {
			t.Fatal(e)
		}
		want := float64(200)
		if info {
			want = 204
		}
		if event["status"] != want {
			t.Fatalf("wrong final status: %v", event)
		}
	}
}
