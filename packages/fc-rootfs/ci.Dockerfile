FROM python:3.10-bookworm

RUN sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources && \
  sed -i 's/http:/https:/g' /etc/apt/sources.list.d/debian.sources

RUN DEBIAN_FRONTEND=noninteractive apt-get update && apt-get install -y --no-install-recommends \
  build-essential curl git util-linux jq

RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple

ENV PIP_DEFAULT_TIMEOUT=100 \
  PIP_DISABLE_PIP_VERSION_CHECK=1 \
  PIP_NO_CACHE_DIR=1 \
  JUPYTER_CONFIG_PATH="/home/user/.jupyter" \
  IPYTHON_CONFIG_PATH="/home/user/.ipython"

COPY ./ci-requirements.txt requirements.txt
RUN pip install --no-cache-dir -r requirements.txt && \
  ipython kernel install --name "python3" --user

COPY ./jupyter_server_config.py $JUPYTER_CONFIG_PATH/

RUN mkdir -p $IPYTHON_CONFIG_PATH/profile_default
COPY ipython_kernel_config.py $IPYTHON_CONFIG_PATH/profile_default/

COPY ./start-up.sh ./start_up.py $JUPYTER_CONFIG_PATH/
RUN chmod +x $JUPYTER_CONFIG_PATH/start-up.sh $JUPYTER_CONFIG_PATH/start_up.py 
