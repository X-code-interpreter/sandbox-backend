# Sandbox Backend

Sandbox Backend enables you to run AI agent in an isolated VM in your infrastructure. After deploying the sandbox backend, you can interact with sandbox using the [Python SDK](https://github.com/X-code-interpreter/sandbox-sdk).

## Installation

Currently, the sandbox backend only able to deploy on Linux.
To install the sandbox backend, you need the following prerequisite software:

- [golang](https://go.dev/doc/install) >= 1.23
- docker
- [Firecracker](https://github.com/firecracker-microvm/firecracker) or [Cloud Hypervisor](https://github.com/cloud-hypervisor/cloud-hypervisor)
- Guest Linux Kernel

Then start compiling the binary of sandbox backend

```bash
git clone https://github.com/X-code-interpreter/sandbox-backend.git
cd sandbox-backend/packages

cd cli && make build && cd -
cd envd && make build && cd -
cd orchestrator && make build && cd -
cd template-manager && make build && cd -
```

Finally, we start to install the python sdk:
```bash
git clone https://github.com/X-code-interpreter/sandbox-sdk.git
cd sandbox-sdk && make install

```

## Quick Start

There are two separate steps to deploy your sandbox, the first is to create the template, the second is to start (multiple) instances of that template.

### Create the template

You need to define the template via the json file, an example is as following:
```bash
cat <<EOF > sandbox-backend/packages/template-manager/default-sandbox.json
{
  "template": "default-sandbox",
  "startCmd": "",
  "memMB": 2048,
  "vcpu": 1,
  "diskMB": 2048,
  "kernelVersion": "6.1.134",
  "hugePages": false,
  "dockerImg": "jialianghuang/default-sandbox:latest",
  "noPull": true,
  "overlay": false,
  "startCmdEnvFilePath": "",
  "startCmdWorkingDirectory": "",
  "vmmType": "firecracker",
  "rootfsBuildMode": 0,
  "kernelDebugOutput": false,
  "hypervisorPath": "/root/codes/firecracker/build/cargo_target/x86_64-unknown-linux-musl/release/firecracker"
}

EOF
```

Then you can use `template-manager` to build the template:

```bash
cd sandbox-backend/packages/template-manager && ./bin/template-manager --template default-sandbox.json
```

Internally, the template-manager will create the block storage and generate a snapshot of the VM.

### Start the sandbox

The let's start the sandbox from the template. First, we need to start the sandbox-backend.

Currently, sandbox-backend only support running on single machine (instead of a cluster).

```bash
cd sandbox-backend/scripts && bash start.sh
```

Now, we can start a sandbox using either the Python SDK or the command line tool.
(Note that we can start multiple sandboxes of the same template).

```python
import asyncio
from sandbox_sdk.sandbox import Sandbox

async def main():
  template = "default-sandbox"
  ci = await Sandbox.create(
      template=template,
      target_addr=SANDBOX_BACKEND_ADDR,
  )
  p = await ci.process.start("python -c \"print('Hello World')\"")
  await p.wait()
  print(p.stdout)


asyncio.run(main())
```

```bash
cd sandbox-backend/packages/cli
./bin/sandbox-cli sandbox create -t default-sandbox
./bin/sandbox-cli sandbox ls -a

# assume the sandbox id is bc94913a-c86f-4a28-8e98-88dd6794b8e1
ssh root@bc94913a-c86f-4a28-8e98-88dd6794b8e1
```


## Customize template
To customize the template, you need to prepare two things:

- A customized docker image
- The template json specification


The meanings of customizable fields in json specification are:
```
{
  "template": # The template name, any string is fine
  "startCmd": # The program you want to run when start the sandbox (e.g., the jupyter notebook)
  "memMB": 2048,
  "vcpu": 1,
  "diskMB": 2048,
  "kernelVersion": # Do not forget to prepare the corresponding guest kernel
  "hugePages": false,
  "dockerImg": # The docker image name
  "noPull": # Should we pull from docker registry or use the local docker image directly
  "overlay": # Use overlayfs or not (which can reduce the page cache usage)
  "startCmdEnvFilePath": # You can specify the environment file for the `startCmd` (i.e., the systemd env file schema).
  "startCmdWorkingDirectory": # The cwd of `startCmd`
  "vmmType": # "firecracker" or "cloud-hypervisor"
  "rootfsBuildMode": 0,
  "kernelDebugOutput": false,
  "hypervisorPath": # The path to the hypervisor binary
}
```


## Acknowledgement
This project partially refers to [E2B](https://e2b.dev/).
