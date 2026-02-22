#!/bin/sh
set -e

if [ "$1" = "remove" ]; then
    systemctl stop xe-exporter || true
    systemctl disable xe-exporter || true
fi
