// Copyright 2022 E99p1ant. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
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
	f := flamego.Classic()
	flamego.SetEnv(flamego.EnvTypeProd)

	f.Any("/", func(ctx flamego.Context) {
		start := time.Now()

		namespace := ctx.Request().Header.Get(Namespace)
		serviceName := ctx.Request().Header.Get(ServiceName)

		defaultExts, err := mime.ExtensionsByType(defaultFormat)
		if err != nil || len(defaultExts) == 0 {
			panic("couldn't get file extension for default format")
		}
		defaultExt := defaultExts[0]
		ext := defaultExt

		format := ctx.Request().Header.Get(FormatHeader)
		if format == "" {
			format = defaultFormat
			log.Printf("format not specified. Using %v", format)
		}

		cext, err := mime.ExtensionsByType(format)
		if err != nil {
			log.Printf("unexpected error reading media type extension: %v. Using %v", err, ext)
			format = defaultFormat
		} else if len(cext) == 0 {
			log.Printf("couldn't get media type extension. Using %v", ext)
		} else {
			ext = cext[0]
		}
		ctx.ResponseWriter().Header().Set(ContentType, format)

		errCode := ctx.Request().Header.Get(CodeHeader)
		code, err := strconv.Atoi(errCode)
		if err != nil {
			code = http.StatusNotFound
			log.Printf("unexpected error reading return code: %v. Using %v", err, code)
		}
		ctx.ResponseWriter().WriteHeader(code)

		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}

		// special case for compatibility
		if ext == ".htm" {
			ext = ".html"
		}

		path := "/www"
		file := filepath.Join(path, namespace, serviceName, fmt.Sprintf("%v%v", code, ext))
		f, err := os.Open(file)
		if err != nil {
			log.Printf("unexpected error opening file: %v", err)

			statusCode := strconv.Itoa(code)
			file := filepath.Join(path, namespace, serviceName, fmt.Sprintf("%cxx%v", statusCode[0], ext))
			f, err := os.Open(file)
			if err != nil {
				log.Printf("unexpected error opening file: %v", err)
				http.NotFound(ctx.ResponseWriter(), ctx.Request().Request)
				return
			}

			defer f.Close()
			log.Printf("serving custom error response for code %v and format %v from file %v", code, format, file)
			_, _ = io.Copy(ctx.ResponseWriter(), f)
			return
		}

		defer f.Close()
		log.Printf("serving custom error response for code %v and format %v from file %v", code, format, file)
		_, _ = io.Copy(ctx.ResponseWriter(), f)

		duration := time.Since(start).Seconds()

		proto := strconv.Itoa(ctx.Request().ProtoMajor)
		proto = fmt.Sprintf("%s.%s", proto, strconv.Itoa(ctx.Request().ProtoMinor))

		requestCount.WithLabelValues(proto).Inc()
		requestDuration.WithLabelValues(proto).Observe(duration)
	})

	f.Any("/metrics", promhttp.Handler())
	f.Any("/healthz", func() {})

	f.Run(8080)
}
