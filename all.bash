#!/bin/sh
set -eux
for p in zoekt zoekt/query zoekt/build ; do
    go test github.com/google/$p
done

for p in zoekt zoekt-webserver zoekt-index zoekt-git-index; do
    go install github.com/google/zoekt/cmd/$p
done
