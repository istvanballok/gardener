#!/bin/bash
# Replace loki with vali in file contents

git grep -z -l loki -- ':!/vendor' ':!/.scripts' ':!NOTICE.md' \
| xargs -0 sed -i 's/loki/vali/g'
