#!/bin/sh
set -eux
for p in zoekt zoekt/query zoekt/build zoekt/git zoekt/web ; do
    go test github.com/google/$p
done

for p in zoekt zoekt-webserver zoekt-server \
    zoekt-index zoekt-git-index zoekt-repo-index zoekt-mirror-github \
    zoekt-mirror-gitiles; do
    go install github.com/google/zoekt/cmd/$p
done
