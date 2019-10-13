#!/usr/bin/env fish

if test (count $argv) -lt 1
  printf "Usage: ./build.fish <version>\n"
  exit 1
end

set -l _version $argv[1]
set -l _commit (git rev-parse --verify HEAD)

go build -ldflags "-X main.gVersion=$_version -X main.gCommit=$_commit"
