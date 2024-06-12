#!/bin/bash
set -euo pipefail

script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
cd $script_dir

if [[ -z "$1" ]]; then
  echo "script requires argument <destination registry>" >&2
  exit 1
fi

DEST_SUBREPO=$1

#export WERF_REPO=ghcr.io/werf/werf-storage

# Extra labels for artifacthub
export WERF_EXPORT_ADD_LABEL_AH1=io.artifacthub.package.readme-url=https://raw.githubusercontent.com/werf/werf/main/README.md \
       WERF_EXPORT_ADD_LABEL_AH2=io.artifacthub.package.logo-url=https://raw.githubusercontent.com/werf/website/main/assets/images/werf-logo.svg \
       WERF_EXPORT_ADD_LABEL_AH3=io.artifacthub.package.category=integration-delivery \
       WERF_EXPORT_ADD_LABEL_AH4=io.artifacthub.package.keywords="cli,ci,cd,build,test,deploy,distribute,cleanup" \
       WERF_EXPORT_ADD_LABEL_OC1=org.opencontainers.image.url=https://github.com/werf/werf/tree/main/scripts/werf-in-image \
       WERF_EXPORT_ADD_LABEL_OC2=org.opencontainers.image.source=https://github.com/werf/werf/tree/main/scripts/werf-in-image \
       WERF_EXPORT_ADD_LABEL_OC3=org.opencontainers.image.created=$(date -u +"%Y-%m-%dT%H:%M:%SZ") \
       WERF_EXPORT_ADD_LABEL_OC4=org.opencontainers.image.description="Official image to run werf in containers"

for group in "1.2"; do

  for distro in "ubuntu"; do
    werf export --tag "$DEST_SUBREPO/werf1:$group-$distro" "$group-stable-$distro"
  done

  for channel in "stable"; do
    werf export --tag "$DEST_SUBREPO/werf1:$group-$channel" "$group-$channel-alpine"

    for distro in "ubuntu"; do
      werf export --tag "$DEST_SUBREPO/werf1:$group-$channel-$distro" "$group-$channel-$distro"
    done
  done
done
