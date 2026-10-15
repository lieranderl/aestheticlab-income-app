package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// contentSecurityPolicy allows only the app's own files. 'unsafe-eval' is
// required by Datastar, which evaluates data-* expressions; inline scripts
// and inline styles stay forbidden.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'; manifest-src 'self'"

// securityHeaders sets the security headers on every response; HSTS only
// when the app is served over HTTPS.
func securityHeaders(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		if secure {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// noStore keeps every response except the public assets out of browser and
// proxy caches: signed-in pages, Datastar answers and exports carry names
// and money. It is applied when the header is written, because Datastar's
// SSE writer sets its own Cache-Control.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&noStoreWriter{ResponseWriter: w}, r)
	})
}

// publicPath is what anyone may cache: assets, the service worker, the
// manifest, the offline page and the readiness check.
func publicPath(p string) bool {
	switch p {
	case "/sw.js", "/manifest.webmanifest", "/offline", "/readyz":
		return true
	}
	return strings.HasPrefix(p, "/static/")
}

type noStoreWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *noStoreWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *noStoreWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

// Unwrap gives http.ResponseController the underlying writer (SSE flushes).
func (w *noStoreWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush keeps Datastar's SSE responses streaming.
func (w *noStoreWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// logging writes one structured log line per request; successful readiness
// checks are left out.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if r.URL.Path == "/readyz" && rw.status == http.StatusOK {
			return
		}
		slog.InfoContext(r.Context(), "http request",
			"method", r.Method,
			"path", logValue(r.URL.Path),
			"status", rw.status,
			"duration", time.Since(start),
		)
	})
}

// recovery turns a handler panic into a 500 and a logged stack trace.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic in HTTP handler",
					"method", r.Method,
					"path", logValue(r.URL.Path),
					"panic", fmt.Sprint(rec),
					"stack", string(debug.Stack()),
				)
				if !rw.wroteHeader {
					http.Error(rw, "Внутренняя ошибка", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// logValue encodes line breaks so a request value cannot forge a log line.
func logValue(v string) string {
	return strings.NewReplacer("\r", "%0D", "\n", "%0A").Replace(v)
}

// statusRecorder remembers the response status for logs and recovery.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *statusRecorder) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *statusRecorder) Write(p []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(p)
}

// Unwrap gives http.ResponseController the underlying writer (SSE flushes).
func (rw *statusRecorder) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// Flush keeps Datastar's SSE responses streaming through the recorder.
func (rw *statusRecorder) Flush() {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
