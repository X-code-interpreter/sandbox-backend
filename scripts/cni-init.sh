#!/bin/bash

prepareTcRedirectTap() {
  local url=https://github.com/awslabs/tc-redirect-tap
  if [ -e /opt/cni/bin/tc-redirect-tap ]; then
    echo "already install tc-redirect-tap"
    return
  fi
  git clone --depth 1 $url
  cd tc-redirect-tap
  make tc-redirect-tap
  make install
  cd -
  rm -rf tc-redirect-tap
}

prepareStandardCNIPlugin() {
  local cniVer="v1.5.0"
  local url="https://github.com/containernetworking/plugins/releases/download/v1.5.0/cni-plugins-linux-amd64-${cniVer}.tgz"
  if [ -e /opt/cni/bin/ptp ] && [ -e /opt/cni/bin/firewall ]; then
    echo "already install standard cni plugin"
    return
  fi
  mkdir -p /opt/cni/bin
  curl -s -L -O --output-dir /opt/cni \
    https://github.com/containernetworking/plugins/releases/download/${cniVer}/cni-plugins-linux-amd64-{cniVer}.tgz
  tar -xf /opt/cni/cni-plugins-linux-amd64-${cniVer}.tgz -C /opt/cni/bin
}

prepareStandardCNIPlugin
prepareTcRedirectTap
