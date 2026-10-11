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
  404.html: |
    <!doctype html><html lang="en"><head><meta charset="utf-8"><title>PAGE NOT FOUND</title></head><body>PAGE NOT FOUND</body></html>
  503.html: |
    <!doctype html><html lang="en"><head><meta charset="utf-8"><title>CUSTOM SERVICE UNAVAILABLE</title></head><body>CUSTOM SERVICE UNAVAILABLE</body></html>

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
              readOnly: true

      # Mounting custom error page from configMap
      volumes:
        - name: custom-error-pages
          configMap:
            name: custom-error-pages
```

The ConfigMap keys are filenames: `404.html` is mounted as `/www/404.html`.
Replace the sample HTML with your own page; HTML and inline CSS can be minified before adding them to the ConfigMap.
The read-only mount covers the entire `/www` directory and hides the image's bundled pages.
Add any required fallback pages, such as `4xx.html` or `5xx.html`, to the same ConfigMap.

To update a page, edit the ConfigMap and reapply the manifest, or commit the change to your GitOps repository.
Kubernetes propagates mounted ConfigMap updates with a delay; this server reads the updated files without a restart.
Keep the directory mount without `subPath`, which does not receive these updates.
See [mounted ConfigMap updates](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#mounted-configmaps-are-updated-automatically).

Both probes request `/healthz` directly on the Pod's port `8080`, without going through the ingress controller.
The readiness probe stops traffic to an unready Pod; the liveness probe restarts an unhealthy container.
See [Kubernetes probe configuration](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).

2. Run `kubectl apply -n kube-system -f custom-error.yaml`.

3. Edit the `ingress-nginx-controller` Deployment and set the value of the `--default-backend-service` flag to the name
   of the newly created error backend.

4. Edit the `ingress-nginx-controller` ConfigMap and create the key `custom-http-errors` with a value of `404,503`.

## Getting started with Traefik

This setup adds low-priority catch-all routes on the `web` and `websecure` entryPoints.
They serve a custom 404 only when no application router matches the request's host and path.
Responses from matched application services, including 404 and 5xx errors, are passed through unchanged.
Individual application Ingress resources do not need middleware annotations.

1. Deploy the ConfigMap, Service and Deployment from step 1 above with
   `kubectl apply -n default -f custom-error.yaml`. The examples below use namespace `default`
   and the error-page Service `nginx-errors`. Ensure Traefik's `Middleware` and `IngressRoute` CRDs
   are installed and its Kubernetes CRD provider watches this namespace.
   For this fallback-only setup, the ConfigMap only needs `404.html`; you can omit `503.html`.
   The page is served from the read-only `/www` mount, so changing its HTML does not require rebuilding the image.

2. Save the following as `traefik-errors.yaml`, then run `kubectl apply -f traefik-errors.yaml`:

```yaml
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
An unmatched path can reach the fallback even when its host has other application routes.
The `error-page-not-found` middleware rewrites unmatched paths to `/404.html`, including paths
such as `/metrics` and `/healthz`, so the fallback does not expose the error server's management endpoints.

The HTTPS route uses Traefik's available certificate, falling back to its default certificate.
Clients still need a certificate valid for the requested hostname; an HTTP error page cannot handle TLS handshake failures.
If HTTP-to-HTTPS redirection is enabled, HTTP requests follow that redirect before reaching the HTTPS fallback.

3. Ensure the Kubernetes CRD provider is enabled in your Traefik Helm values:

```yaml
providers:
  kubernetesCRD:
    enabled: true
```

Preserve your existing Helm values and upgrade the Traefik release if you change this setting.
Do not attach an Errors middleware to the entryPoints or application routers for this fallback-only setup.
If you previously installed the global Errors configuration, remove its middleware references from the entryPoints
and application routers first. Wait for the Traefik Helm rollout to complete before deleting the unused Errors middleware,
so pods running the old configuration do not lose their middleware references during the transition.

The fallback requests `/404.html` from this server and uses `/www/404.html`, falling back to `/www/4xx.html`.
The `/{code}.html` endpoint always serves HTML and takes the status from its URL, ignoring forwarded Nginx-specific headers.

See Traefik's [IngressRoute](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/crd/http/ingressroute/)
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
