FROM gcr.io/distroless/static:nonroot

COPY ingress-custom-error-pages  /
COPY www /www
USER nonroot:nonroot

CMD ["/ingress-custom-error-pages"]
