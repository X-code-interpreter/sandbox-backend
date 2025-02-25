FROM python:3.10.12-bookworm

RUN sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources && \
  sed -i 's/http:/https:/g' /etc/apt/sources.list.d/debian.sources

RUN DEBIAN_FRONTEND=noninteractive apt-get update && apt-get install -y --no-install-recommends \
  build-essential curl git util-linux jq less iproute2 vim coreutils && \
  apt clean

RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple
  # pip config set install.trusted-host mirrors.aliyun.com

COPY ./agent-requirements.txt requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

