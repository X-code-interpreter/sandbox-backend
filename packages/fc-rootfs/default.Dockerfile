FROM python:3.10-bookworm

RUN sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources && \
  sed -i 's/http:/https:/g' /etc/apt/sources.list.d/debian.sources

RUN DEBIAN_FRONTEND=noninteractive apt-get update && apt-get install -y --no-install-recommends \
  build-essential curl git util-linux jq

RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple

ENV PIP_DEFAULT_TIMEOUT=100 \
  PIP_DISABLE_PIP_VERSION_CHECK=1 \
  PIP_NO_CACHE_DIR=1

COPY ./default-requirements.txt requirements.txt
RUN pip install --no-cache-dir -r requirements.txt
