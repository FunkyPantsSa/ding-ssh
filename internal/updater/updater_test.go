package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetRejectsHTTPFailure(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "missing", 404) }))
	defer s.Close()
	if _, e := get(context.Background(), s.Client(), s.URL); e == nil {
		t.Fatal("accepted missing update")
	}
}
func TestCheckRejectsUnknownChannelBeforeNetwork(t *testing.T) {
	_, e := Check(context.Background(), "../../main")
	if e == nil || !strings.Contains(e.Error(), "无效") {
		t.Fatal(e)
	}
}
func TestDownloadRejectsUnsafeAsset(t *testing.T) {
	m := Manifest{Assets: map[string]Asset{}}
	if _, e := Download(context.Background(), m, func(int64, int64) {}); e == nil {
		t.Fatal("accepted absent platform")
	}
}
