# Build stage
FROM golang:1-alpine AS builder

ARG COREDNS_VERSION=v1.14.0
ARG PLUGIN_MODULE=github.com/etcdhosts/etcdhosts/v2

# Install build dependencies
RUN apk add --no-cache git make

WORKDIR /src

# Clone CoreDNS source
RUN git clone --depth 1 --branch ${COREDNS_VERSION} https://github.com/coredns/coredns.git coredns

# Copy etcdhosts plugin source
COPY . /src/etcdhosts

WORKDIR /src/coredns

# Add etcdhosts plugin to plugin.cfg (before hosts plugin)
RUN sed -i '/^hosts:hosts/i\etcdhosts:'"${PLUGIN_MODULE}" plugin.cfg

# Add replace directive to use local source
RUN go mod edit -replace=${PLUGIN_MODULE}=/src/etcdhosts

# Generate plugin code
RUN make gen

# Build CoreDNS with etcdhosts plugin
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /coredns

# Runtime stage
FROM mritd/alpine

# Copy coredns binary
COPY --from=builder /coredns /coredns

# Copy example Corefile
COPY Corefile.example /etc/coredns/Corefile

WORKDIR /
EXPOSE 53 53/udp
ENTRYPOINT ["/coredns"]
CMD ["-conf", "/etc/coredns/Corefile"]
