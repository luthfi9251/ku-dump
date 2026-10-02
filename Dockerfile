FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
RUN CGO_ENABLED=0 go build -o /ku-dump ./cmd/ku-dump

FROM postgres:16
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && curl -fsSL -o /tmp/tools.deb https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-x86_64-100.11.0.deb \
 && apt-get install -y /tmp/tools.deb \
 && rm -f /tmp/tools.deb \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /ku-dump /usr/local/bin/ku-dump
ENV KUDUMP_ADDR=:8080 KUDUMP_DB=/data/ku-dump.db KUDUMP_DUMPS_DIR=/data/dumps
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ku-dump"]
