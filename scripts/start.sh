#!/bin/bash

SCRIPT=$(realpath "${BASH_SOURCE[0]}")
SCRIPT_PATH=$(dirname "$SCRIPT")
PKG_PATH="$(dirname $SCRIPT_PATH)/packages"
CGROUP_NAME=${CGROUP_NAME:-sandbox-backend}
CGROUP_ROOT=/sys/fs/cgroup/${CGROUP_NAME}

declare -a BG_TASK_PID=()

function prepare_kvm() {
  # check group kvm is in current users, if not, add it to current user
  if ! groups $USER | grep -q "\bkvm\b"; then
    echo "ask sudo to add current user to kvm group"
    sudo usermod -aG kvm $USER
    newgrp kvm
  fi
}

function prepare_net() {
  echo "ask sudo to allow user $USER access to /run/netns and /etc/hosts"
  sudo -s -- <<EOF
mkdir -p /run/netns
setfacl -m u:$USER:rwx /run/netns
setfacl -m u:$USER:rw /etc/hosts
EOF
}

function prepare_cgroup() {
  if [ ! -d $CGROUP_ROOT ]; then
    echo "ask sudo to create $CGROUP_NAME under /sys/fs/cgroup"
    sudo -s -- <<EOF
mkdir ${CGROUP_ROOT}
chown $USER ${CGROUP_ROOT}
chown $USER ${CGROUP_ROOT}/cgroup.subtree_control
chown $USER ${CGROUP_ROOT}/cgroup.procs
EOF
    local controllers=$(<"$CGROUP_ROOT/cgroup.controllers")
    for ctrl in $controllers; do
      echo "+$ctrl" >"$CGROUP_ROOT/cgroup.subtree_control"
    done
  fi
}

function start_docker_service() {
  pushd ${SCRIPT_PATH}
  # -s means stop container before remove
  # -f means do not confirm before remove
  docker compose rm -s -f prometheus
  # clean previous data
  if docker volume list | grep "ci-prometheus-data"; then
    echo "start remove prometheus volume..."
    docker volume rm ci-prometheus-data
  fi
  docker compose up --detach --force-recreate
  popd
}

function stop_docker_service() {
  pushd ${SCRIPT_PATH}
  docker compose down
  popd
}

# and start log-collector
function start_log_collecator() {
  pushd ${PKG_PATH}/log-collector
  cgexec ${CGROUP_NAMA}/test ./bin/log-collector \
    --config ${PKG_PATH}/config.toml &>/tmp/log-collector.log &
  local pid=$!
  echo "log collector (pid ${pid}) log is in /tmp/log-collector.log"
  popd
  BG_TASK_PID+=($pid)
}

function rebuild() {
  pushd ${PKG_PATH}/orchestrator
  echo "start to build orchestrator..."
  make build
  popd

  pushd ${PKG_PATH}/log-collector
  echo "start to build log collector..."
  make build
  popd

  pushd ${PKG_PATH}/cli
  echo "start to build cli..."
  make build
  popd

  pushd ${PKG_PATH}/template-manager
  echo "start to build template manager..."
  make build
  popd
}

function start_orchestrator() {
  pushd ${PKG_PATH}/orchestrator
  ENVIRONMENT=prod cgexec ${CGROUP_NAME}/test ./bin/orchestrator \
    --config ${PKG_PATH}/config.toml &>/tmp/orchestrator.log &
  local pid=$!
  echo "orchestrator (pid ${pid}) log is in /tmp/orchestrator.log"
  popd
  BG_TASK_PID+=($pid)
}

function main() {
  local subcommand=$1
  if [ -z "$subcommand" ]; then
    echo "Usage: $0 <subcommand>"
    echo "Available subcommands: setup, rebuild, stop, run"
    exit 1
  fi

  if [ "$subcommand" == "setup" ]; then
    prepare_cgroup
    prepare_kvm
    prepare_net
    rebuild
  elif [ "$subcommand" == "rebuild" ]; then
    rebuild
  elif [ "$subcommand" == "stop" ]; then
    stop_docker_service
  elif [ "$subcommand" == "run" ]; then
    start_docker_service
    sleep 5
    start_log_collecator
    start_orchestrator
    # wait until finish
    echo "bg task pid" "${BG_TASK_PID[*]}"
    for _pid in "${BG_TASK_PID[@]}"; do
      echo "waiting pid $_pid"
      wait $_pid
    done
  else
    echo "unknown subcommand: $subcommand"
    exit 1
  fi
}

main $@
