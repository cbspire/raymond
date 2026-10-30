package manager

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogProjectionRemovesSecrets(t *testing.T) {
	for _, line := range []string{
		`time=2026 level=INFO msg=request method=GET host=studio.test path=/safe status=200 bytes=12 duration_ms=1 authorization=private cookie=private`,
		`2026/10/30 17:19:00 INFO request method=GET host=studio.test path=/safe?token=private status=200 cookie=private`,
		`2026-10-30T17:19:00Z runner raymond[12]: 2026/10/30 17:19:00 INFO request method=GET host=studio.test path="/safe?token=private" status=200`,
	} {
		entry := ParseRequestLog(line)
		if entry == nil || entry["path"] != "/safe" {
			t.Fatal("missing access event", line, entry)
		}
		b, e := json.Marshal(entry)
		if e != nil || strings.Contains(string(b), "private") {
			t.Fatal("leaked log data", string(b), e)
		}
	}
	for _, line := range []string{`native command failed password=secret`, `msg=request status=999 path=/`, `msg=request status=200 path="unterminated`} {
		if ParseRequestLog(line) != nil {
			t.Fatal("accepted unsafe line", line)
		}
	}
}
