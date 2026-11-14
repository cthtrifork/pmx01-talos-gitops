# fileserver: one static Go binary, four modes (see README.md), no third-party dependencies. The final image is scratch
# plus CA certificates (the rotator talks TLS to the Kubernetes API), so there is no OS to patch.
ARG GO_IMAGE=mirror.gcr.io/library/golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523
FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fileserver .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/fileserver /fileserver
USER 65532:65532
ENTRYPOINT ["/fileserver"]
