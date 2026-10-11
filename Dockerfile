# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/ingress-custom-error-pages .

FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/ingress-custom-error-pages /ingress-custom-error-pages
COPY www /www
USER 65532:65532

EXPOSE 8080

CMD ["/ingress-custom-error-pages"]
