// Copyright 2026 E99p1ant. All rights reserved..
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultFormat = "text/html"
)

func init() {
	prometheus.MustRegister(requestCount)
	prometheus.MustRegister(requestDuration)
}

func main() {
	flamego.SetEnv(flamego.EnvTypeProd)

	root, err := os.OpenRoot("/www")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	newServer(root).Run(8080)
}

func newServer(root *os.Root) *flamego.Flame {
	f := flamego.Classic()

	serveError := func(ctx flamego.Context) {
		start := time.Now()
		req := ctx.Request()
		proto := strconv.Itoa(req.ProtoMajor) + "." + strconv.Itoa(req.ProtoMinor)
		defer func() {
			requestCount.WithLabelValues(proto).Inc()
			requestDuration.WithLabelValues(proto).Observe(time.Since(start).Seconds())
		}()

		namespace := req.Header.Get(Namespace)
		serviceName := req.Header.Get(ServiceName)
		requested := req.Header.Get(FormatHeader)
		errCode := req.Header.Get(CodeHeader)

		if pathCode := ctx.Param("code"); pathCode != "" {
			// Traefik forwards client headers, so its error-page URL defines the response.
			query := req.URL.Query()
			namespace = query.Get("namespace")
			serviceName = query.Get("service")
			requested = defaultFormat
			errCode = pathCode
		}

		for _, name := range []string{namespace, serviceName} {
			if name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
				http.Error(ctx.ResponseWriter(), "invalid namespace or service name", http.StatusBadRequest)
				return
			}
		}

		format, ext := defaultFormat, ".html"
		if requested != "" && requested != defaultFormat {
			if mediaType, _, err := mime.ParseMediaType(requested); err == nil && mediaType != defaultFormat {
				if extensions, err := mime.ExtensionsByType(mediaType); err == nil && len(extensions) > 0 {
					format, ext = mediaType, extensions[0]
				}
			}
		}

		code, err := strconv.Atoi(errCode)
		if err != nil || code < 400 || code > 599 {
			code = http.StatusNotFound
		}

		file, err := tryToOpenFile(root, errorPagePaths(namespace, serviceName, code, ext))
		if err != nil && format != defaultFormat {
			format = defaultFormat
			file, err = tryToOpenFile(root, errorPagePaths(namespace, serviceName, code, ".html"))
		}

		if err != nil {
			log.Printf("couldn't find custom error page for code %d: %v", code, err)
			http.Error(ctx.ResponseWriter(), http.StatusText(code), code)
			return
		}

		defer func() {
			if err := file.Close(); err != nil {
				log.Printf("unexpected error closing file: %v", err)
			}
		}()

		ctx.ResponseWriter().Header().Set(ContentType, format)
		ctx.ResponseWriter().WriteHeader(code)

		if _, err := io.Copy(ctx.ResponseWriter(), file); err != nil {
			log.Printf("unexpected error writing custom error response: %v", err)
		}
	}

	f.Any("/", serveError)
	f.Any("/{code}.html", serveError)

	f.Any("/metrics", promhttp.Handler())
	f.Any("/healthz", func() {})

	return f
}

func errorPagePaths(namespace, serviceName string, code int, ext string) []string {
	dirs := make([]string, 0, 3)
	if serviceName != "" {
		dirs = append(dirs, filepath.Join(namespace, serviceName))
	}
	if namespace != "" {
		dirs = append(dirs, namespace)
	}
	dirs = append(dirs, "")

	statusCode := strconv.Itoa(code)
	paths := make([]string, 0, 2*len(dirs))
	for _, dir := range dirs {
		paths = append(paths, filepath.Join(dir, statusCode+ext), filepath.Join(dir, statusCode[:1]+"xx"+ext))
	}

	return paths
}

func tryToOpenFile(root *os.Root, paths []string) (*os.File, error) {
	lastErr := os.ErrNotExist
	for _, path := range paths {
		f, err := root.Open(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Printf("unexpected error opening file: %v", err)
				lastErr = err
			}
			continue
		}

		info, err := f.Stat()
		if err == nil && info.Mode().IsRegular() {
			return f, nil
		}

		if closeErr := f.Close(); closeErr != nil {
			log.Printf("unexpected error closing file: %v", closeErr)
		}

		if err != nil {
			log.Printf("unexpected error checking file: %v", err)
			lastErr = err
		}
	}

	return nil, lastErr
}
