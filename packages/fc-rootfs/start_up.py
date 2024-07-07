import asyncio
import aiohttp
from aiohttp import ClientSession
from jupyter_client.manager import AsyncKernelManager
import os
import json
from typing import Dict
import subprocess
import time
import glob

BASE_URL = "http://localhost:8888"
SESSION_PATH = os.path.expanduser("~")
SESSION_KERNEL_NAME = "python3"
# import some popular packages
JUPYTER_CMD = """
import matplotlib
import numpy
import matplotlib.pyplot
import pandas
import seaborn
import sklearn
"""


async def wait_for_server(s: ClientSession):
    start_time = time.time()
    counter = 0
    url = f"{BASE_URL}/api/status"
    while True:
        try:
            async with s.get(url) as resp:
                if resp.status == 200:
                    break
        except Exception as e:
            pass
        counter += 1
        if counter % 20 == 0:
            print("Waiting for Jupyter Server to start...")
        await asyncio.sleep(0.1)
    end_time = time.time()
    print(f"wait for jupyter server start take {end_time - start_time} second")


async def create_session(s: ClientSession):
    payload = {
        "path": SESSION_PATH,
        "kernel": {"name": SESSION_KERNEL_NAME},
        "type": "notebook",
        "name": "default",
    }
    async with s.post(f"{BASE_URL}/api/sessions", json=payload) as resp:
        session_info = await resp.json()
        status_code = resp.status
    kernel_id = session_info["kernel"]["id"]
    exec_state = session_info["kernel"]["execution_state"]
    if exec_state != "starting":
        raise Exception(f"error creating kernel: {session_info} {status_code}")
    with open(os.path.join(SESSION_PATH, ".jupyter", "kernel_id"), "w") as f:
        f.write(kernel_id)
    with open(os.path.join(SESSION_PATH, ".jupyter", ".session_info"), "w") as f:
        json.dump(session_info, f)
    return session_info


async def execute_cmd_in_jupyter(session_info: Dict, cmd: str):
    runtime_dir = os.path.join(SESSION_PATH, ".local", "share", "jupyter", "runtime")
    connection_files = glob.glob(os.path.join(runtime_dir, "kernel-*.json"))
    assert len(connection_files) == 1
    km = AsyncKernelManager()
    km.connection_file = connection_files[0]
    km.load_connection_file()
    client = km.client()
    client.start_channels()

    start_time = time.time()
    msg_id = client.execute(cmd)

    async def handle_iopub_msg():
        iopub_msg = await client.get_iopub_msg()
        print(f"recv iopub msg {iopub_msg}")

    t = asyncio.create_task(handle_iopub_msg())
    reply = await client._recv_reply(msg_id, timeout=60)

    end_time = time.time()
    print(f"execute finished: {reply}, elapsed {end_time - start_time} seconds")
    t.cancel()



async def main():
    async with aiohttp.ClientSession() as s:
        await wait_for_server(s)
        session_info = await create_session(s)
        await execute_cmd_in_jupyter(session_info, JUPYTER_CMD)
    await asyncio.sleep(100)


if __name__ == "__main__":
    p = subprocess.Popen(
        ["jupyter", "server", "--IdentityProvider.token="],
        start_new_session=True,
    )
    asyncio.run(main())
    p.wait()
