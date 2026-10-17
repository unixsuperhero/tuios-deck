#!/usr/bin/env bash
# Stands in for tuios under the extension check: appends its argv, one JSON
# line per call, to $FAKE_TUIOS_LOG.
printf '%s\n' "$3" >> "$FAKE_TUIOS_LOG"
