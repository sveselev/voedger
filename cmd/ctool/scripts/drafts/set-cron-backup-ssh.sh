#!/usr/bin/env bash
#
# Copyright (c) 2024 Sigma-Soft, Ltd.
# @author Dmitry Molchanovsky
# 
# writes a database backup task to cron
# over an ssh connection
set -euo pipefail
set -x

if [ $# -ne 2 ]; then
  echo "Usage: $0 <cron schedule time> <ssh port>" 
  exit 1
fi

source ./utils.sh

SCHEDULE=$1
SSH_PORT=$2
SSH_USER=$LOGNAME
CRON_HOST_NAME="app-node-1"
CRON_HOST=$(nslookup ${CRON_HOST_NAME} | awk '/^Address: / { print $2 }')

REMOTE_COMMAND="bash -s \"${SCHEDULE}\" ${SSH_PORT}"

utils_ssh -t "${SSH_USER}"@"${CRON_HOST}" "${REMOTE_COMMAND}" < set-cron-backup.sh

set +x