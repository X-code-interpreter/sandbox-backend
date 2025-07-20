#!/bin/bash

# example usage:
# OUTPUT_DIR=<data_root>/kernels ./build.sh

# please use absolute path for OUTPUT_DIR
OUTPUT_DIR=${OUTPUT_DIR:-/mnt/pmem1/hjl/sandbox-data/kernels}

set -euo pipefail

function confirm_prompt() {
  local prompt="$1"
  local response

  while true; do
    read -rp "$prompt [y/N]: " response </dev/tty
    case "$response" in
    [yY][eE][sS] | [yY]) return 0 ;;
    [nN][oO] | [nN]) return 1 ;;
    *) echo "Please answer yes or no." ;;
    esac
  done
}

function build_version {
  local full_ver="$1"

  # Validate version format X.Y.Z
  if [[ ! "$full_ver" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "Error: version must be in X.Y.Z format"
    return 1
  fi
  local major_minor="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}"
  local linux_dir="linux-${full_ver}"
  local msg_tag="v${full_ver}"

  # # Find configs matching major or full version
  mapfile -t configs < <(find $(realpath ./configs) -type f -regextype posix-extended \
    -regex ".*/[^/]+-(${major_minor}|${full_ver})\.config")
  if ((${#configs[@]} == 0)); then
    echo "No configs for ${major_minor} or full version found."
    return 1
  fi

  echo "Configs to build: ${configs[*]}"
  if ! confirm_prompt "try to clone linux $msg_tag into $linux_dir"; then
    echo "build for $full_ver declined"
    return 1
  fi

  # Clone minimal linux-stable source
  if [[ ! -d "$linux_dir" ]]; then
    git clone --depth 1 --branch "$msg_tag" \
      https://mirrors.tuna.tsinghua.edu.cn/git/linux-stable.git \
      "$linux_dir"
  fi

  pushd "$linux_dir" >/dev/null
  for cfg in "${configs[@]}"; do
    local cfg_base=$(basename "$cfg" .config)

    local prefix="${cfg##*/}"
    prefix="${prefix%.config}"
    local out_subdir
    if [[ "$prefix" == *"$full_ver" ]]; then
      out_subdir="${OUTPUT_DIR}/${prefix}"
    else
      # replace major_minor at the end with full_ver
      out_subdir="${OUTPUT_DIR}/${prefix%$major_minor}$full_ver"
    fi

    echo $cfg
    if ! confirm_prompt "try to build at $(pwd) with $(basename $cfg), output to $out_subdir"; then
      echo "compile for $cfg declined"
      continue
    fi

    mkdir -p "$out_subdir"

    make mrproper
    cp "$cfg" .config
    make olddefconfig
    make -j"$(nproc)"

    # Copy vmlinux to output
    if [[ -f vmlinux ]]; then
      cp vmlinux "${out_subdir}/vmlinux"
      echo "Copied vmlinux to ${out_subdir}/"
    else
      echo "Warning: vmlinux not found after build." >&2
    fi
  done
  popd >/dev/null

  # Clean up
  rm -rf "$linux_dir"
  echo "Removed source directory $linux_dir"
}

grep -v '^ *#' <./kernel_versions.txt | while IFS= read -r version; do
  echo $version
  build_version "$version"
done
