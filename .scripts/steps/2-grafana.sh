#!/bin/bash
# Replace the Grafana container image with that of Plutono

git grep -z -l "repository: grafana/grafana" -- ':!/vendor' ':!/.scripts' ':!NOTICE.md' \
| xargs -0 sed -i -E 's|repository: grafana/grafana|repository: ghcr.io/credativ/plutono|
                      s/tag: "7.5.17"/tag: "main" # TODO/'
