package main

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func testRoot(t *testing.T, files map[string]string) *os.Root {
	t.Helper()

	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })

	return root
}

func errorRequest(code, format string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(CodeHeader, code)
	req.Header.Set(FormatHeader, format)

	return req
}

func TestErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name, code, format, body, contentType string
		status                                int
		jsonPage, noPages                     bool
	}{
		{name: "HTML", code: "503", format: "text/html", status: 503, body: "server error", contentType: "text/html"},
		{name: "default format", code: "503", status: 503, body: "server error", contentType: "text/html"},
		{name: "HTML parameters", code: "503", format: "text/html; charset=utf-8", status: 503, body: "server error", contentType: "text/html"},
		{name: "wildcard", code: "503", format: "*/*", status: 503, body: "server error", contentType: "text/html"},
		{name: "unknown format", code: "503", format: "application/x-test-unknown", status: 503, body: "server error", contentType: "text/html"},
		{name: "multiple formats", code: "503", format: "application/json, text/html", status: 503, body: "server error", contentType: "text/html"},
		{name: "JSON", code: "503", format: "application/json", status: 503, body: `{"error":"unavailable"}`, contentType: "application/json", jsonPage: true},
		{name: "missing JSON", code: "503", format: "application/json", status: 503, body: "server error", contentType: "text/html"},
		{name: "missing pages", code: "503", format: "application/json", status: 503, body: "Service Unavailable\n", contentType: "text/plain; charset=utf-8", noPages: true},
		{name: "missing code", status: 404, body: "client error", contentType: "text/html"},
		{name: "malformed code", code: "bad", status: 404, body: "client error", contentType: "text/html"},
		{name: "zero code", code: "0", status: 404, body: "client error", contentType: "text/html"},
		{name: "negative code", code: "-1", status: 404, body: "client error", contentType: "text/html"},
		{name: "code below range", code: "399", status: 404, body: "client error", contentType: "text/html"},
		{name: "code above range", code: "600", status: 404, body: "client error", contentType: "text/html"},
		{name: "four-digit code", code: "1000", status: 404, body: "client error", contentType: "text/html"},
		{name: "lowest error code", code: "400", status: 400, body: "client error", contentType: "text/html"},
		{name: "highest error code", code: "599", status: 599, body: "server error", contentType: "text/html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"4xx.html": "client error", "5xx.html": "server error"}
			if tc.noPages {
				files = nil
			} else if tc.jsonPage {
				files["503.json"] = `{"error":"unavailable"}`
			}

			w := httptest.NewRecorder()
			newServer(testRoot(t, files)).ServeHTTP(w, errorRequest(tc.code, tc.format))

			if w.Code != tc.status || w.Body.String() != tc.body || w.Result().Header.Get(ContentType) != tc.contentType {
				t.Fatalf("got status=%d, type=%q, body=%q; want status=%d, type=%q, body=%q",
					w.Code, w.Result().Header.Get(ContentType), w.Body.String(), tc.status, tc.contentType, tc.body)
			}
		})
	}
}

func TestErrorPagePriority(t *testing.T) {
	paths := []string{"team/app/503.html", "team/app/5xx.html", "team/503.html", "team/5xx.html", "503.html", "5xx.html"}
	files := make(map[string]string, len(paths))
	for _, path := range paths {
		files[path] = path
	}

	root := testRoot(t, files)
	server := newServer(root)

	for _, path := range paths {
		req := errorRequest("503", "text/html")
		req.Header.Set(Namespace, "team")
		req.Header.Set(ServiceName, "app")

		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != 503 || w.Body.String() != path {
			t.Fatalf("got status=%d, body=%q; want 503, %q", w.Code, w.Body.String(), path)
		}

		if err := root.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTraefikErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		status           int
		forwardedHeaders bool
	}{
		{name: "exact page", path: "/503.html", status: 503, body: "global page"},
		{name: "status class fallback", path: "/502.html", status: 502, body: "server error"},
		{name: "not found page", path: "/404.html", status: 404, body: "not found page"},
		{name: "namespace page", path: "/503.html?namespace=team", status: 503, body: "namespace page"},
		{name: "service page", path: "/503.html?namespace=team&service=app", status: 503, body: "service page"},
		{name: "scope fallback", path: "/503.html?namespace=missing&service=missing", status: 503, body: "global page"},
		{name: "ignore forwarded headers", path: "/503.html", status: 503, body: "global page", forwardedHeaders: true},
		{name: "explicit scope wins", path: "/503.html?namespace=team&service=app", status: 503, body: "service page", forwardedHeaders: true},
		{name: "zero code", path: "/0.html", status: 404, body: "not found page"},
		{name: "out-of-range code", path: "/600.html", status: 404, body: "not found page"},
		{name: "four-digit code", path: "/1000.html", status: 404, body: "not found page"},
		{name: "malformed code", path: "/bad.html", status: 404, body: "not found page"},
		{name: "namespace traversal", path: "/503.html?namespace=..%2Fprivate", status: 400, body: "invalid namespace or service name\n"},
		{name: "service traversal", path: "/503.html?namespace=team&service=..%5Cprivate", status: 400, body: "invalid namespace or service name\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testRoot(t, map[string]string{
				"404.html":          "not found page",
				"503.html":          "global page",
				"5xx.html":          "server error",
				"team/503.html":     "namespace page",
				"team/app/503.html": "service page",
			})
			server := httptest.NewServer(newServer(root))
			defer server.Close()

			req, err := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}

			if tc.forwardedHeaders {
				req.Header.Set(CodeHeader, "404")
				req.Header.Set(FormatHeader, "application/json")
				req.Header.Set(Namespace, "../private")
				req.Header.Set(ServiceName, "other")
			}

			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}

			contentType := defaultFormat
			if tc.status == http.StatusBadRequest {
				contentType = "text/plain; charset=utf-8"
			}

			if resp.StatusCode != tc.status || string(body) != tc.body || resp.Header.Get(ContentType) != contentType {
				t.Fatalf("got status=%d, type=%q, body=%q; want status=%d, type=%q, body=%q",
					resp.StatusCode, resp.Header.Get(ContentType), body, tc.status, contentType, tc.body)
			}
		})
	}
}

func TestRejectInvalidPathComponents(t *testing.T) {
	server := newServer(testRoot(t, map[string]string{"5xx.html": "fallback"}))

	for _, header := range []string{Namespace, ServiceName} {
		for _, name := range []string{".", "..", "../private", "/tmp", "team/app", `team\app`} {
			t.Run(header+"="+name, func(t *testing.T) {
				req := errorRequest("503", "text/html")
				req.Header.Set(header, name)

				w := httptest.NewRecorder()
				server.ServeHTTP(w, req)

				if w.Code != http.StatusBadRequest {
					t.Fatalf("got %d, want 400", w.Code)
				}
			})
		}
	}
}

func TestErrorFileFallback(t *testing.T) {
	for _, kind := range []string{"directory", "external symlink", "ConfigMap symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := testRoot(t, map[string]string{"5xx.html": "fallback", "..version/503.html": "ConfigMap page"})
			path := filepath.Join(root.Name(), "503.html")
			want := "fallback"
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "external symlink":
				outside := filepath.Join(t.TempDir(), "503.html")
				if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "ConfigMap symlink":
				if err := os.Symlink("..version", filepath.Join(root.Name(), "..data")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("..data/503.html", path); err != nil {
					t.Fatal(err)
				}
				want = "ConfigMap page"
			}

			w := httptest.NewRecorder()
			newServer(root).ServeHTTP(w, errorRequest("503", "text/html"))

			if w.Code != 503 || w.Body.String() != want {
				t.Fatalf("got status=%d, body=%q; want 503, %q", w.Code, w.Body.String(), want)
			}
		})
	}
}

func TestUniqueErrorPagePaths(t *testing.T) {
	for _, tc := range []struct {
		namespace, service string
		count              int
	}{{"", "", 2}, {"team", "", 4}, {"", "app", 4}, {"team", "app", 6}} {
		paths := errorPagePaths(tc.namespace, tc.service, 503, ".html")
		seen := make(map[string]bool)
		for _, path := range paths {
			if seen[path] {
				t.Fatalf("duplicate candidate %q", path)
			}
			seen[path] = true
		}

		if len(paths) != tc.count {
			t.Fatalf("got %v, want %d candidates", paths, tc.count)
		}
	}
}

func TestMissingCandidatesDoNotLog(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	root := testRoot(t, map[string]string{"5xx.html": "fallback"})
	file, err := tryToOpenFile(root, []string{"missing.html", "5xx.html"})
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := tryToOpenFile(root, []string{"missing.html"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want file-not-found error", err)
	}
	if output.Len() != 0 {
		t.Fatalf("missing candidates produced logs: %s", output.String())
	}
}

type failingResponseWriter struct{ *httptest.ResponseRecorder }

func (w failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("test write failure")
}

func TestCopyErrorIsLogged(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	w := failingResponseWriter{httptest.NewRecorder()}
	newServer(testRoot(t, map[string]string{"503.html": "page"})).ServeHTTP(w, errorRequest("503", "text/html"))

	if !strings.Contains(output.String(), "test write failure") {
		t.Fatalf("write failure not logged: %s", output.String())
	}
}

type delayedResponseWriter struct{ *httptest.ResponseRecorder }

func (w delayedResponseWriter) Write(body []byte) (int, error) {
	time.Sleep(10 * time.Millisecond)
	return w.ResponseRecorder.Write(body)
}

func TestErrorRequestMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(requestCount, requestDuration)

	for _, tc := range []struct {
		name, path string
		files      map[string]string
	}{
		{name: "Nginx page found", path: "/", files: map[string]string{"503.html": "page"}},
		{name: "Nginx page missing", path: "/"},
		{name: "Traefik page found", path: "/503.html", files: map[string]string{"503.html": "page"}},
		{name: "Traefik page missing", path: "/503.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestCount.Reset()
			requestDuration.Reset()

			server := newServer(testRoot(t, tc.files))
			req := errorRequest("503", "text/html")
			req.URL.Path = tc.path

			start := time.Now()
			server.ServeHTTP(delayedResponseWriter{httptest.NewRecorder()}, req)
			elapsed := time.Since(start).Seconds()

			families, err := registry.Gather()
			if err != nil {
				t.Fatal(err)
			}
			if len(families) != 2 {
				t.Fatalf("got %d metric families, want 2", len(families))
			}

			for _, family := range families {
				metric := family.GetMetric()[0]
				switch family.GetName() {
				case "default_http_backend_http_request_count_total":
					if metric.GetCounter().GetValue() != 1 {
						t.Fatal("request was not counted")
					}
				case "default_http_backend_http_request_duration_seconds":
					histogram := metric.GetHistogram()
					if histogram.GetSampleCount() != 1 || histogram.GetSampleSum() < .01 || histogram.GetSampleSum() > elapsed {
						t.Fatalf("duration is missing or not in seconds: %v (elapsed %f)", histogram, elapsed)
					}
				default:
					t.Fatalf("unexpected metric %q", family.GetName())
				}
			}
		})
	}
}

func TestHealthAndMetricsRoutes(t *testing.T) {
	server := httptest.NewServer(newServer(testRoot(t, nil)))
	defer server.Close()

	for _, path := range []string{"/healthz", "/metrics"} {
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s returned %d, want 200", path, resp.StatusCode)
		}
	}
}
