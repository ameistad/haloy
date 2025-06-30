#!/bin/sh

CERT_DIR="/usr/local/etc/haproxy/certs"
DUMMY_CERT="${CERT_DIR}/dummy.pem"

if [ ! -f "$DUMMY_CERT" ]; then
    echo "No certificate found. Generating a self-signed dummy certificate..."
    mkdir -p "$CERT_DIR"
    openssl req -x509 -nodes -days 1 -newkey rsa:2048 \
      -keyout "$DUMMY_CERT" -out "$DUMMY_CERT" \
      -subj "/CN=localhost"
    echo "Dummy certificate generated."
fi

echo "Starting HAProxy..."
exec haproxy -f /usr/local/etc/haproxy/config/haproxy.cfg "$@"
