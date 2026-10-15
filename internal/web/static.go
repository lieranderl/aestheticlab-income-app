package web

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// staticHandler serves the embedded assets. Cloud Run does not compress
// responses, so text assets (CSS, JS, SVG, manifest) are gzipped once at
// start-up and sent that way to browsers that accept gzip; fonts and PNGs are
// already compressed and go out as they are.
func staticHandler(files fs.FS) (http.Handler, error) {
	zipped := map[string][]byte{}
	err := fs.WalkDir(files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !compressible(name) {
			return err
		}
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := zw.Write(raw); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		zipped[name] = buf.Bytes()
		return nil
	})
	if err != nil {
		return nil, err
	}
	plain := http.FileServer(http.FS(files))
	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := zipped[r.URL.Path]
		if !ok {
			plain.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Vary", "Accept-Encoding")
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			plain.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", textTypes[path.Ext(r.URL.Path)])
		w.Header().Set("Content-Encoding", "gzip")
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(body))
	})), nil
}

// textTypes are the compressed asset types, spelled out because the
// distroless image has no MIME database and Go has no .webmanifest entry.
var textTypes = map[string]string{
	".css":         "text/css; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".svg":         "image/svg+xml",
	".webmanifest": "application/manifest+json",
}

func compressible(name string) bool {
	_, ok := textTypes[path.Ext(name)]
	return ok
}

// acceptsGzip reads Accept-Encoding loosely: gzip listed and not refused (q=0).
func acceptsGzip(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			q := strings.ReplaceAll(params, " ", "")
			return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
		}
	}
	return false
}
