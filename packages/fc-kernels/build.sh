#!/bin/bash

BASE_DIR=/mnt/data/X-code-interpreter

set -euo pipefail

function build_version {
  local version=$1
  local major_version=$(echo "$version" | cut -d '.' -f 1-2)
  echo "Starting build for kernel version: $version"

  if [ -e "../configs/${version}.config" ]; then
    echo "using ${version}.config"
    cp ../configs/"${version}.config" .config
  elif [ -e "../configs/${major_version}.config" ]; then
    echo "using ${major_version}.config"
    cp ../configs/"${major_version}.config" .config
  else
    echo "No matching kernel config find for $version"
    return
  fi

  echo "Checking out repo for kernel at version: $version"
  git fetch --depth 1 origin "v${version}"
  git checkout FETCH_HEAD

  echo "Building kernel version: $version"
  make olddefconfig
  make vmlinux -j "$(nproc)"

  echo "Copying finished build to builds directory"
  # mkdir -p "../builds/vmlinux-${version}"
  mkdir -p ${BASE_DIR}/fc-kernels/${version}
  cp vmlinux "${BASE_DIR}/fc-kernels/${version}/vmlinux"
}

echo "Cloning the linux kernel repository"
git clone --depth 1 https://mirrors.tuna.tsinghua.edu.cn/git/linux-stable.git linux
cd linux

grep -v '^ *#' <../kernel_versions.txt | while IFS= read -r version; do
  build_version "$version"
done

cd ..
rm -rf linux
