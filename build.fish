#!/usr/bin/env fish

if test (count $argv) -lt 1
  printf "Usage: ./build.fish <version>\n"
  exit 1
end

set -l _version $argv[1]
set -l _commit (git rev-parse --verify HEAD)

printf "Building version %s, commit %s\n" $_version $_commit

podman build \
  -t kcm_build \
  --no-cache \
  --build-arg version=$_version \
  --build-arg commit=$_commit .

set -l _container_name (podman create kcm_build /bin/true)
podman cp $_container_name:/kcm ./kcm
