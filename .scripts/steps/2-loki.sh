#!/bin/bash
# Replace the Loki container image with that of Vali

git grep -z -l "repository: grafana/loki" -- ':!/vendor' ':!/.scripts' ':!NOTICE.md' \
| xargs -0 sed -i -E 's|repository: grafana/loki|repository: ghcr.io/credativ/vali|
                      s|repository: "docker.io/grafana/promtail"|repository: "ghcr.io/credativ/valitail"|
                      s/tag: "2.2.1"/tag: "main" # TODO/'
