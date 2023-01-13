#!/bin/bash
# Replace the Loki and Promtail container images with that of Vali and Valitail
#
# The loki and promtail images are going to be mirrored in the Gardener GCR
# registry by concourse after this PR is merged. Then, we can switch to use the
# Gardener GCR registry instead of the Credativ GHCR registry.

git grep -z -l "repository: eu.gcr.io/gardener-project/3rd/grafana/loki" -- ':!/vendor' ':!/.scripts' ':!NOTICE.md' \
| xargs -0 sed -i -E 's|repository: eu.gcr.io/gardener-project/3rd/grafana/loki|repository: ghcr.io/credativ/vali|
                      s|repository: eu.gcr.io/gardener-project/3rd/grafana/promtail|repository: ghcr.io/credativ/valitail|
                      s/tag: "2.2.1"/tag: "v2.2.5"/'
