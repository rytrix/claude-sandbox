FROM docker.io/library/alpine:3.22
RUN apk add --no-cache tinyproxy
COPY tinyproxy.conf allowlist /etc/tinyproxy/
CMD ["tinyproxy", "-d", "-c", "/etc/tinyproxy/tinyproxy.conf"]
