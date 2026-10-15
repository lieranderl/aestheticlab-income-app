package web_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lieranderl/aestheticlab-income-app/internal/web"
	"github.com/lieranderl/aestheticlab-income-app/static"
)

// Text assets go out gzipped when the phone accepts it (Cloud Run does not
// compress), and unchanged when it does not.
func TestStaticAssetsAreGzipped(t *testing.T) {
	h := handler(t, web.Config{})
	for _, name := range []string{"app.css", "datastar.js"} {
		want, err := static.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/static/"+name+"?v=2", nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s gzip: status %d, encoding %q, vary %q", name, rec.Code, rec.Header().Get("Content-Encoding"), rec.Header().Get("Vary"))
		}
		if rec.Body.Len()*2 > len(want) {
			t.Errorf("%s gzip: %d bytes for %d", name, rec.Body.Len(), len(want))
		}
		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(zr)
		if err != nil || string(got) != string(want) {
			t.Fatalf("%s gzip body does not match the file (err %v)", name, err)
		}

		req = httptest.NewRequest(http.MethodGet, "/static/"+name, nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != string(want) {
			t.Fatalf("%s without gzip: encoding %q, %d bytes", name, rec.Header().Get("Content-Encoding"), rec.Body.Len())
		}
	}
}

// Go has no built-in type for .webmanifest and the distroless image has no
// MIME database, so the gzipped manifest must still say what it is.
func TestGzippedManifestKeepsItsContentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/static/manifest.webmanifest", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler(t, web.Config{}).ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Fatalf("Content-Type = %q, want application/manifest+json", got)
	}
}
