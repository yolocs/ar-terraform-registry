FROM --platform=$BUILDPLATFORM golang:1.23 AS builder

FROM cgr.dev/chainguard/static

COPY ar-terraform-registry /ar-terraform-registry

ENV PORT 8080
ENTRYPOINT ["/ar-terraform-registry"]
