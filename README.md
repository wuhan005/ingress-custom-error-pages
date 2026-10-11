# 😵 ingress-custom-error-pages ![Go](https://github.com/wuhan005/ingress-custom-error-pages/workflows/Go/badge.svg) [![Sourcegraph](https://img.shields.io/badge/view%20on-Sourcegraph-brightgreen.svg?logo=sourcegraph)](https://sourcegraph.com/github.com/wuhan005/go-template)

English | [简体中文](README.zh-CN.md)

Customized error pages for Nginx Ingress and Traefik.

## Getting started with Nginx Ingress

1. Create file `custom-error.yaml` with the following contents:

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-error-pages
data:
  404: |
    <!DOCTYPE html>
    <html>
      <head><title>PAGE NOT FOUND</title></head>
      <body>PAGE NOT FOUND</body>
    </html>
  503: |
    <!DOCTYPE html>
    <html>
      <head><title>CUSTOM SERVICE UNAVAILABLE</title></head>
      <body>CUSTOM SERVICE UNAVAILABLE</body>
    </html>
---
apiVersion: v1
kind: Service
metadata:
  name: nginx-errors
  labels:
    app.kubernetes.io/name: nginx-errors
    app.kubernetes.io/part-of: ingress-nginx
spec:
  selector:
    app.kubernetes.io/name: nginx-errors
    app.kubernetes.io/part-of: ingress-nginx
  ports:
    - port: 80
      targetPort: 8080
      name: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-errors
  labels:
    app.kubernetes.io/name: nginx-errors
    app.kubernetes.io/part-of: ingress-nginx
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: nginx-errors
      app.kubernetes.io/part-of: ingress-nginx
  template:
    metadata:
      labels:
        app.kubernetes.io/name: nginx-errors
        app.kubernetes.io/part-of: ingress-nginx
    spec:
      containers:
        - name: nginx-error-server
          image: ghcr.io/wuhan005/ingress-custom-error-pages:latest
          imagePullPolicy: Always
          ports:
            - containerPort: 8080

          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
            periodSeconds: 5
            timeoutSeconds: 2
            failureThreshold: 3

          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
            timeoutSeconds: 2
            failureThreshold: 3

          # Mounting custom error page from configMap
          volumeMounts:
            - name: custom-error-pages
              mountPath: /www

      # Mounting custom error page from configMap
      volumes:
        - name: custom-error-pages
          configMap:
            name: custom-error-pages
            items:
              - key: "404"
                path: "404.html"
              - key: "503"
                path: "503.html"
```

Both probes request `/healthz` directly on the Pod's port `8080`, without going through the ingress controller.
The readiness probe stops traffic to an unready Pod; the liveness probe restarts an unhealthy container.
See [Kubernetes probe configuration](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).

2. Run `kubectl apply -n kube-system -f custom-error.yaml`.

3. Edit the `ingress-nginx-controller` Deployment and set the value of the `--default-backend-service` flag to the name
   of the newly created error backend.

4. Edit the `ingress-nginx-controller` ConfigMap and create the key `custom-http-errors` with a value of `404,503`.

## Getting started with Traefik

This setup applies custom error pages globally to the `web` and `websecure` entryPoints.
Low-priority catch-all routes also handle requests that do not match an application router.
Individual application Ingress resources do not need middleware annotations.

1. Deploy the ConfigMap, Service and Deployment from step 1 above with
   `kubectl apply -n default -f custom-error.yaml`. The examples below use namespace `default`
   and the error-page Service `nginx-errors`. Ensure Traefik's `Middleware` and `IngressRoute` CRDs
   are installed and its Kubernetes CRD provider watches this namespace.

2. Save the following as `traefik-errors.yaml`, then run `kubectl apply -f traefik-errors.yaml`:

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: custom-errors
  namespace: default
spec:
  errors:
    status:
      - "400-599"
    query: "/{status}.html"
    service:
      name: nginx-errors
      port: 80
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: error-page-not-found
  namespace: default
spec:
  replacePath:
    path: /404.html
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: error-page-fallback-http
  namespace: default
spec:
  entryPoints:
    - web
  routes:
    - kind: Rule
      match: PathPrefix(`/`)
      priority: 1
      middlewares:
        - name: error-page-not-found
      services:
        - name: nginx-errors
          port: 80
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: error-page-fallback-https
  namespace: default
spec:
  entryPoints:
    - websecure
  routes:
    - kind: Rule
      match: PathPrefix(`/`)
      priority: 1
      middlewares:
        - name: error-page-not-found
      services:
        - name: nginx-errors
          port: 80
  tls: {}
```

The fallback routes use priority `1`; application routes must have a higher priority.
The `error-page-not-found` middleware rewrites unmatched paths to `/404.html`, including paths
such as `/metrics` and `/healthz`, so the fallback does not expose the error server's management endpoints.

The HTTPS route uses Traefik's available certificate, falling back to its default certificate.
Clients still need a certificate valid for the requested hostname; an HTTP error page cannot handle TLS handshake failures.
If HTTP-to-HTTPS redirection is enabled, HTTP requests follow that redirect before reaching the HTTPS fallback.

3. Merge these settings into your existing Traefik Helm values and upgrade the Traefik release:

```yaml
providers:
  kubernetesCRD:
    enabled: true

additionalArguments:
  - "--entryPoints.web.http.middlewares=default-custom-errors@kubernetescrd"
  - "--entryPoints.websecure.http.middlewares=default-custom-errors@kubernetescrd"
```

Preserve your existing Helm values and additional arguments. If an entryPoint already has middlewares,
prepend `default-custom-errors@kubernetescrd` to its existing comma-separated middleware list
instead of defining the same option twice. The prefix `default-` is the middleware's namespace;
change it along with the resource namespaces if you deploy elsewhere.

Traefik must restart with the updated entryPoint settings. The Errors middleware then handles 400–599
responses from all routers on these entryPoints, including application middleware errors.
Remove any per-Ingress references to the same Errors middleware to avoid applying it twice.

Traefik replaces `{status}` with the response status: a 503 requests `/503.html` from this server.
This global configuration selects pages directly under `/www`, such as `/www/404.html`, `/www/503.html`
or `/www/5xx.html`. The `/{code}.html` endpoint always serves HTML and takes the status from its URL,
ignoring forwarded Nginx-specific headers.

See Traefik's [Errors middleware](https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/errorpages/),
[entryPoint middlewares](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/#httpmiddlewares),
[IngressRoute](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/crd/http/ingressroute/)
and [ReplacePath middleware](https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/replacepath/) documentation.

## Error page fallback

Pages are searched in service, namespace, then global directories under `/www`.
Within each directory, the exact status code (such as `503.html`) takes precedence over the status class (`5xx.html`).
Missing files and directories are skipped. Namespace and service headers must be single path components;
invalid components return HTTP 400, and files cannot be read through symlinks pointing outside `/www`.

HTML always uses the `.html` extension. Unknown formats and missing pages for a requested format fall back to HTML.
If no page exists, the server returns plain text with the original error status.
Missing, malformed, or out-of-range status codes (`X-Code` for Nginx Ingress, the URL code for Traefik)
fall back to 404; supported error codes are 400–599.

## Metrics

`/metrics` exposes request counts and durations for error-page requests, including requests that fail to find a page.
Durations use seconds: `default_http_backend_http_request_duration_seconds`.
Update dashboards and alerts that use the former `default_http_backend_http_request_duration_milliseconds`
metric to use the new name; the old implementation already recorded values in seconds.

## Why not [ingress-nginx official custom error pages](https://github.com/kubernetes/ingress-nginx/tree/main/images/custom-error-pages)?

The official custom error pages only provides a simple HTTP server to serve the static error page files, which doesn't
support displaying different pages based on different Kubernetes namespaces or services.

## Acknowledgments

[kubernetes/ingress-nginx custom-error-pages](https://github.com/kubernetes/ingress-nginx/tree/main/images/custom-error-pages)

## License

MIT License
