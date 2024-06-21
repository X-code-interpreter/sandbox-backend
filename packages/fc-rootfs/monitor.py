import psutil
import time
import logging
from logging.handlers import RotatingFileHandler
import json
import argparse

MAX_MB = 200

# Configure logging with rotation
log_formatter = logging.Formatter("%(message)s")
log_handler = RotatingFileHandler(
    "/root/metrics_log.json", maxBytes=MAX_MB * 1024 * 1024, backupCount=2
)
log_handler.setFormatter(log_formatter)
log_handler.setLevel(logging.INFO)

logger = logging.getLogger("metrics_logger")
logger.setLevel(logging.INFO)
logger.addHandler(log_handler)


def collect_metrics():
    metrics = {
        "timestamp": time.time(),
        "cpu": psutil.cpu_percent(interval=None, percpu=True),
        "memory": psutil.virtual_memory().percent,
        "network": psutil.net_io_counters(pernic=True),
    }
    return metrics


def main(interval):
    while True:
        metrics = collect_metrics()
        logger.info(json.dumps(metrics))
        time.sleep(interval)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "-i",
        "--interval",
        type=int,
        default=1,
        help="interval between metrics collection in second (default: 1)",
    )
    args = parser.parse_args()
    main(args.interval)
