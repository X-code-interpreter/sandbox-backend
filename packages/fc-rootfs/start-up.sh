#!/bin/bash

function start_jupyter_server() {
  counter=0
  response=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:8888/api/status")
  while [[ ${response} -ne 200 ]]; do
    let counter++
    if  (( counter % 20 == 0 )); then
      echo "Waiting for Jupyter Server to start..."
      sleep 0.1
    fi

    response=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:8888/api/status")
  done
  echo "Jupyter Server started"

  response=$(curl -s -X POST "localhost:8888/api/sessions" -H "Content-Type: application/json" -d '{"path": "/home/user", "kernel": {"name": "python3"}, "type": "notebook", "name": "default"}')
  status=$(echo "${response}" | jq -r '.kernel.execution_state')
  if [[ ${status} != "starting" ]]; then
    echo "Error creating kernel: ${response} ${status}"
    exit 1
  fi
  echo "Kernel created"

  mkdir /home/user/.jupyter
  kernel_id=$(echo "${response}" | jq -r '.kernel.id')
  echo "${kernel_id}" > /home/user/.jupyter/kernel_id
  echo "${response}" > /home/user/.jupyter/.session_info
  echo "Jupyter Server started"
}

echo "Starting Jupyter Server..."
start_jupyter_server &
jupyter server --IdentityProvider.token=""
