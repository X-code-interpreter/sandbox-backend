FROM python:3.9-bookworm

RUN sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources && \
  sed -i 's/http:/https:/g' /etc/apt/sources.list.d/debian.sources

RUN DEBIAN_FRONTEND=noninteractive apt-get update && apt-get install -y --no-install-recommends \
  build-essential curl git util-linux jq \
  libgomp1 chromium fonts-ipafont-gothic fonts-wqy-zenhei fonts-thai-tlwg fonts-kacst fonts-freefont-ttf libxss1 && \
  apt clean

RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple
  # pip config set install.trusted-host mirrors.aliyun.com

COPY ./metagpt-requirements.txt requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

WORKDIR /root
COPY ./config2.yaml /root/.metagpt/config2.yaml
# install necessary submodule
RUN https_proxy=http://127.0.0.1:7890 git clone https://github.com/huang-jl/MetaGPT.git && \
  cd MetaGPT && pip install -e . && \
  pip install -e .[search-ddg]

