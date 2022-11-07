# 😵 ingress-custom-error-pages ![Go](https://github.com/wuhan005/ingress-custom-error-pages/workflows/Go/badge.svg) [![Go Report Card](https://goreportcard.com/badge/github.com/wuhan005/ingress-custom-error-pages)](https://goreportcard.com/report/github.com/wuhan005/ingress-custom-error-pages) [![Sourcegraph](https://img.shields.io/badge/view%20on-Sourcegraph-brightgreen.svg?logo=sourcegraph)](https://sourcegraph.com/github.com/wuhan005/go-template)

Customized Ingress error pages.

## Getting started

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
          image: registry.cn-hangzhou.aliyuncs.com/eggplant/ingress-custom-error-pages:latest
          imagePullPolicy: Always
          ports:
            - containerPort: 8080

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

2. Run `kubectl apply -n kube-system -f custom-error.yaml`.

3. Edit the `ingress-nginx-controller` Deployment and set the value of the `--default-backend-service` flag to the name
   of the newly created error backend.

4. Edit the `ingress-nginx-controller` ConfigMap and create the key `custom-http-errors` with a value of `404,503`.

## Why not [ingress-nginx official custom error pages](https://github.com/kubernetes/ingress-nginx/tree/main/images/custom-error-pages)?

The official custom error pages only provides a simple HTTP server to serve the static error page files, which doesn't
support displaying different pages based on different Kubernetes namespaces or services.

## Acknowledgments

[kubernetes/ingress-nginx custom-error-pages](https://github.com/kubernetes/ingress-nginx/tree/main/images/custom-error-pages)

## License

MIT License
