#!/bin/sh
set -e

if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
    systemctl daemon-reload || true
fi
if [ "$1" = "purge" ]; then
    # Delete any potential logs if necessary
    true
fi
