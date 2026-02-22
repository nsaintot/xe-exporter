#!/bin/sh
set -e

if [ "$1" = "configure" ]; then
    systemctl daemon-reload || true
    if ! systemctl is-enabled xe-exporter >/dev/null 2>&1; then
        systemctl enable xe-exporter || true
    fi
    systemctl restart xe-exporter || true
fi
