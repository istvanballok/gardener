#!/bin/bash
# Replace the Grafana container image with that of Plutono
#
# The plutono image is going to be mirrored in the Gardener GCR registry by
# concourse after this PR is merged. Then, we can switch to use the Gardener GCR
# registry instead of the Credativ GHCR registry.

git grep -z -l "repository: eu.gcr.io/gardener-project/3rd/grafana/grafana" -- ':!/vendor' ':!/.scripts' ':!NOTICE.md' \
| xargs -0 sed -i -E 's|repository: eu.gcr.io/gardener-project/3rd/grafana/grafana|repository: ghcr.io/credativ/plutono|
                      s/tag: "7.5.17"/tag: "v7.5.21"/'
