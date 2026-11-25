# syntax=docker/dockerfile:1
# Build context: repository root. Targets: fakeacct, app (default).

FROM node:24 AS ui
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.27 AS go
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/fakeacct

FROM go AS sor
COPY --from=ui /ui/dist/ cmd/sor/dist/
RUN go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/sor && /out/sor check --static

FROM alpine:3.22 AS typst
ADD --checksum=sha256:a6d077d0a95eed5a2eba715b2dae06be954f624ccbf85758a03f389ded33118c \
    https://github.com/typst/typst/releases/download/v0.15.1/typst-x86_64-unknown-linux-musl.tar.xz /typst.tar.xz
RUN tar -xJf /typst.tar.xz --strip-components=1 -C /usr/local/bin typst-x86_64-unknown-linux-musl/typst

FROM debian:trixie-slim AS base
RUN useradd --system --uid 10001 --no-create-home sor

FROM base AS fakeacct
COPY --from=go /out/fakeacct /usr/local/bin/
USER sor
CMD ["fakeacct"]

FROM base AS app
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tesseract-ocr tesseract-ocr-fra tesseract-ocr-deu tesseract-ocr-eng \
 && rm -rf /var/lib/apt/lists/*
COPY --from=pgplex/pgschema:2.0.0 /usr/local/bin/pgschema /usr/local/bin/
COPY --from=typst /usr/local/bin/typst /usr/local/bin/
COPY --from=sor /out/sor /usr/local/bin/
USER sor
CMD ["sor", "up"]
