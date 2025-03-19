FROM python:3.12.3-bookworm

RUN sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources && \
  sed -i 's/http:/https:/g' /etc/apt/sources.list.d/debian.sources

RUN DEBIAN_FRONTEND=noninteractive apt-get update && apt-get install -y --no-install-recommends \
  build-essential curl git util-linux jq less iproute2 vim coreutils \
  ffmpeg libsm6 libxext6 && \
  apt clean

RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple
  # pip config set install.trusted-host mirrors.aliyun.com

RUN pip install --no-cache-dir uv

COPY ./owl-requirements.txt requirements.txt
RUN uv venv /home/user/venv --seed && . /home/user/venv/bin/activate && \
  pip install --no-cache-dir uv && uv pip install -r requirements.txt \
  -C--global-option=build_ext -C--global-option=-j16 \
  --extra-index-url https://download.pytorch.org/whl/cpu \
  --index-strategy unsafe-best-match

# pre-download the toktoken files
RUN . /home/user/venv/bin/activate && \
  python -c "import tiktoken; tiktoken.get_encoding('o200k_base'); tiktoken.get_encoding('cl100k_base')"

# install playwright and its dependency
ENV PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright
RUN . /home/user/venv/bin/activate && \
  playwright install chromium && playwright install-deps

COPY ./camel /home/user/camel
WORKDIR /home/user/camel
RUN . /home/user/venv/bin/activate && uv pip install \
  --extra-index-url https://download.pytorch.org/whl/cpu\
  --index-strategy unsafe-best-match\
  -e .[all]

COPY ./owl /home/user/owl
WORKDIR /home/user/owl
RUN . /home/user/venv/bin/activate && uv pip install \
  --extra-index-url https://download.pytorch.org/whl/cpu\
  --index-strategy unsafe-best-match\
  -e .
