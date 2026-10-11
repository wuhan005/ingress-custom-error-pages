# 😵 ingress-custom-error-pages ![Go](https://github.com/wuhan005/ingress-custom-error-pages/workflows/Go/badge.svg) [![Sourcegraph](https://img.shields.io/badge/view%20on-Sourcegraph-brightgreen.svg?logo=sourcegraph)](https://sourcegraph.com/github.com/wuhan005/go-template)

[English](README.md) | 简体中文

为 Nginx Ingress 和 Traefik 提供自定义错误页面。

## Nginx Ingress 快速开始

1. 创建 `custom-error.yaml` 文件，内容如下：

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

          # 挂载 ConfigMap 中的自定义错误页面
          volumeMounts:
            - name: custom-error-pages
              mountPath: /www
              readOnly: true

      # 挂载 ConfigMap 中的自定义错误页面
      volumes:
        - name: custom-error-pages
          configMap:
            name: custom-error-pages
```

ConfigMap 的键名就是文件名，例如 `404.html` 会挂载为 `/www/404.html`。
请将示例 HTML 替换为自己的页面，HTML 和内联 CSS 可以压缩后再放入 ConfigMap。
只读挂载会覆盖整个 `/www` 目录，镜像内置的页面将不可见。
如需 `4xx.html`、`5xx.html` 等回退页面，也应将它们放入同一个 ConfigMap。

更新页面时，修改 ConfigMap 并重新应用清单，或将修改提交到 GitOps 仓库。
Kubernetes 会在一段延迟后同步挂载内容，本服务无需重启即可读取更新后的文件。
请保持目录挂载，不要使用 `subPath`，后者无法接收这些更新。
具体行为可参考 [ConfigMap 挂载更新文档](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#mounted-configmaps-are-updated-automatically)。

两个探针都直接请求 Pod 的 `8080` 端口上的 `/healthz`，不经过入口控制器。
就绪检查（readinessProbe）失败时停止向该 Pod 分发流量，存活检查（livenessProbe）失败时重启容器。
具体行为可参考 [Kubernetes 探针配置文档](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)。

2. 执行 `kubectl apply -n kube-system -f custom-error.yaml`。

3. 编辑 `ingress-nginx-controller` 的 Deployment，将 `--default-backend-service` 参数设置为新建的错误页面后端服务。

4. 编辑 `ingress-nginx-controller` 的 ConfigMap，添加 `custom-http-errors`，值为 `404,503`。

## Traefik 快速开始

以下配置在 `web` 和 `websecure` 入口（entryPoints）上添加低优先级兜底路由，
仅在请求的 Host 和路径未匹配到应用路由时返回自定义 404。
已匹配应用服务的响应会原样返回，包括服务自身产生的 404 和 5xx 错误。
无需为每个应用的 Ingress 单独添加中间件注解。

1. 使用上方第 1 步的 ConfigMap、Service 和 Deployment，执行
   `kubectl apply -n default -f custom-error.yaml` 部署错误页面服务。
   以下示例使用 `default` 命名空间，错误页面 Service 名为 `nginx-errors`。
   请确保已安装 Traefik 的 `Middleware` 和 `IngressRoute` CRD，且 Kubernetes CRD 提供程序监听该命名空间。
   这套仅处理兜底请求的配置只需在 ConfigMap 中保留 `404.html`，可以省略 `503.html`。
   页面从只读挂载的 `/www` 目录读取，修改 HTML 无需重新构建镜像。

2. 将以下内容保存为 `traefik-errors.yaml`，然后执行 `kubectl apply -f traefik-errors.yaml`：

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

兜底路由的优先级为 `1`，应用路由的优先级应高于该值。
即使 Host 已配置了其他应用路由，没有匹配路由的路径也可能进入兜底。
`error-page-not-found` 中间件将未匹配的请求路径重写为 `/404.html`，包括 `/metrics` 和 `/healthz` 等路径，
避免通过兜底路由暴露错误页面服务的管理接口。

HTTPS 路由使用 Traefik 中可用的证书，没有匹配证书时使用默认证书。
客户端仍需能够验证请求域名的证书；HTTP 错误页面无法处理 TLS 握手失败。
如果已启用 HTTP 到 HTTPS 的重定向，HTTP 请求会先重定向，再由 HTTPS 兜底路由处理。

3. 确保 Traefik Helm values 中已启用 Kubernetes CRD 提供程序：

```yaml
providers:
  kubernetesCRD:
    enabled: true
```

修改该配置时，请保留原有的 Helm values 并升级 Traefik release。
这套仅处理兜底请求的配置不需要在入口或应用路由上挂载 Errors 中间件。
如果之前已安装全局 Errors 配置，请先移除入口和应用路由上的相关中间件引用，
等待 Traefik 的 Helm 滚动更新完成后，再删除不再使用的 Errors 中间件，
避免仍使用旧配置的 Pod 在更新期间因引用失效而无法处理请求。

兜底路由向本服务请求 `/404.html`，使用 `/www/404.html`，不存在时回退到 `/www/4xx.html`。
`/{code}.html` 接口始终返回 HTML，并使用 URL 中的状态码，忽略转发过来的 Nginx 专用请求头。

相关配置可参考 Traefik 官方文档：
[IngressRoute](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/crd/http/ingressroute/)
和 [ReplacePath 中间件](https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/replacepath/)。

## 错误页面回退规则

服务会在 `/www` 下依次查找服务目录、命名空间目录和全局目录。
在每个目录内，优先使用精确状态码页面（如 `503.html`），其次使用状态码类别页面（如 `5xx.html`）。
不存在的文件和目录会被跳过。命名空间和服务名称请求头必须为单个路径组件；
无效的路径组件会返回 HTTP 400，指向 `/www` 之外的符号链接也无法用于读取文件。

HTML 页面始终使用 `.html` 扩展名。未知格式或请求格式对应的页面不存在时，会回退到 HTML。
如果最终仍未找到页面，则返回纯文本，并保留原始错误状态码。
状态码缺失、格式错误或超出范围时，默认使用 404；支持的错误状态码范围为 400–599。
Nginx Ingress 通过 `X-Code` 请求头传递状态码，Traefik 通过 URL 中的状态码传递。

## 监控指标

`/metrics` 提供错误页面请求的次数和耗时指标，包括未找到页面的请求。
耗时以秒为单位，指标名称为 `default_http_backend_http_request_duration_seconds`。
如果现有仪表盘或告警使用了旧指标 `default_http_backend_http_request_duration_milliseconds`，
请更新为新名称；旧实现实际记录的数值也已经是秒。

## 为什么不使用 [ingress-nginx 官方自定义错误页面](https://github.com/kubernetes/ingress-nginx/tree/main/images/custom-error-pages)？

官方自定义错误页面仅提供一个用于返回静态错误页面文件的简单 HTTP 服务，
不支持根据 Kubernetes 命名空间或服务展示不同页面。

## 致谢

[kubernetes/ingress-nginx custom-error-pages](https://github.com/kubernetes/ingress-nginx/tree/main/images/custom-error-pages)

## 许可证

MIT License
