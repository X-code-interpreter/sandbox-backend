#!/bin/bash

SCRIPT=$(realpath "${BASH_SOURCE[0]}")
SCRIPT_PATH=$(dirname "$SCRIPT")
PKG_PATH="$(dirname $SCRIPT_PATH)/packages"

declare -a BG_TASK_PID=()

function start_docker_service() {
  pushd ${SCRIPT_PATH}
  docker compose up --detach --force-recreate
  popd
}

# and start log-collector
function start_log_collecator() {
  pushd ${PKG_PATH}/log-collector
  echo "start to build log collector..."
  make
  ./bin/log-collector &> /tmp/log-collector.log &
  local pid=$!
  echo "log collector (pid ${pid}) log is in /tmp/log-collector.log"
  popd
  BG_TASK_PID+=($pid)
}

function start_orchestrator() {
  pushd ${PKG_PATH}/orchestrator
  echo "start to build orchestrator..."
  make
  ENVIRONMENT=prod ./bin/orchestrator &> /tmp/orchestrator.log &
  local pid=$!
  echo "orchestrator (pid ${pid}) log is in /tmp/orchestrator.log"
  popd
  BG_TASK_PID+=($pid)
}

start_docker_service
sleep 5

start_log_collecator

start_orchestrator

echo "bg task pid" "${BG_TASK_PID[*]}"
for _pid in "${BG_TASK_PID[@]}"; do
  echo "waiting pid $_pid"
  wait $_pid
done
