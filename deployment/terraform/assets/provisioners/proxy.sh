#!/bin/bash

set -euo pipefail

# Wait for boot to be finished (e.g. networking to be up).
while [ ! -f /var/lib/cloud/instance/boot-finished ]; do echo 'Waiting for cloud-init...'; sleep 1; done

# Retry loop (up to 3 times)
n=0
until [ "$n" -ge 3 ]
do
      # Note: commands below are expected to be either idempotent or generally safe to be run more than once.
      echo "Attempt ${n}"
      echo 'tcp_bbr' | sudo tee -a /etc/modules && \
      sudo modprobe tcp_bbr && \
      wget -qO - https://nginx.org/keys/nginx_signing.key | gpg --dearmor | sudo tee /etc/apt/trusted.gpg.d/nginx.gpg && \
      sudo sh -c 'echo "deb [arch=amd64] http://nginx.org/packages/mainline/ubuntu/ $(lsb_release -cs) nginx" > /etc/apt/sources.list.d/nginx.list' && \
      sudo sh -c 'echo "deb-src http://nginx.org/packages/mainline/ubuntu/ $(lsb_release -cs) nginx" >> /etc/apt/sources.list.d/nginx.list' && \
      sudo apt-get -y update && \
      sudo apt-get install -y nginx && \
      sudo apt-get install -y prometheus-node-exporter && \
      sudo apt-get install -y numactl linux-tools-aws linux-tools-aws-lts-22.04 && \
      sudo systemctl daemon-reload && \
      sudo systemctl enable nginx && \
      sudo mkdir -p /etc/nginx/snippets && \
      sudo mkdir -p /etc/nginx/sites-available && \
      sudo mkdir -p /etc/nginx/sites-enabled && \
      sudo rm -f /etc/nginx/sites-enabled/default && \
      sudo ln -fs /etc/nginx/sites-available/mattermost /etc/nginx/sites-enabled/mattermost && \
      exit 0
   n=$((n+1)) 
   sleep 2
done

echo 'All retry attempts have failed, exiting' && exit 1
