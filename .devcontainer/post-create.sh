#!/bin/env bash

set -e

sudo chown -R vscode:vscode /mnt/mise-data
curl -fsSL https://deb.nodesource.com/setup_25.x |\
    DEBIAN_FRONTEND=noninteractive sudo -E bash -
sudo apt install -y -qq \
    nodejs \
    npm \
    python3.11-venv
node -v
npm -v
mise install -y
hk install 
