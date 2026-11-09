package web

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestLogValuesCannotForgeLines(t *testing.T) {
	if got := logValue("/a\r\nlevel=ERROR\n"); got != "/a%0D%0Alevel=ERROR%0A" {
		t.Fatalf("logValue = %q", got)
	}
	if got := logError(errors.New("bad name \"x\nlevel=ERROR\"")); got != "bad name \"x%0Alevel=ERROR\"" {
		t.Fatalf("logError = %q", got)
	}
	if got := logError(nil); got != "" {
		t.Fatalf("logError(nil) = %q", got)
	}
}

func TestNavigateRefusesOffSiteTargets(t *testing.T) {
	for _, target := range []string{"", "x", "https://evil.example", "//evil.example", "/\\evil.example", "/a\\b", "/a\r\nb"} {
		a := &app{}
		w := httptest.NewRecorder()
		a.navigate(w, httptest.NewRequest("POST", "/", nil), target)
		if w.Code != 500 || w.Header().Get("Location") != "" {
			t.Fatalf("navigate(%q): status %d, Location %q; want a refusal", target, w.Code, w.Header().Get("Location"))
		}
	}
	a := &app{}
	w := httptest.NewRecorder()
	a.navigate(w, httptest.NewRequest("POST", "/", nil), "/my?month=2026-11")
	if w.Code != 303 || w.Header().Get("Location") != "/my?month=2026-11" {
		t.Fatalf("local target: status %d, Location %q", w.Code, w.Header().Get("Location"))
	}
}
