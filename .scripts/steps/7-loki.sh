#!/bin/bash
# Replace Loki with Vali in file contents

git grep -z -l Loki -- ':!/vendor' ':!/.scripts' ':!NOTICE.md' \
| xargs -0 sed -i 's/Loki/Vali/g'
