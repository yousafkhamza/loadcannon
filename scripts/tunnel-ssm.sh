#!/usr/bin/env bash
# tunnel-ssm.sh — reach an internal endpoint without a VPN, using SSM Session
# Manager port forwarding through an instance that already has network access
# (e.g. a bastion, or the app instance itself).
#
# No inbound security group rules or open ports required — auth is via IAM.
#
# Usage:
#   ./scripts/tunnel-ssm.sh -i i-0abcd1234ef567890 -r us-east-1 -h internal-lb.company.internal -p 443 -l 8443
#
# Then point loadcannon at https://localhost:8443 with:
#   target.host_override = "internal-lb.company.internal"
#   target.insecure_skip_verify = true   (cert CN won't match localhost)
set -euo pipefail

INSTANCE_ID=""
REGION="${AWS_REGION:-}"
REMOTE_HOST=""
REMOTE_PORT="443"
LOCAL_PORT="8443"

usage() {
  echo "Usage: $0 -i <instance-id> -r <region> -h <remote-host> [-p <remote-port>] [-l <local-port>]"
  exit 1
}

while getopts "i:r:h:p:l:" opt; do
  case "$opt" in
    i) INSTANCE_ID="$OPTARG" ;;
    r) REGION="$OPTARG" ;;
    h) REMOTE_HOST="$OPTARG" ;;
    p) REMOTE_PORT="$OPTARG" ;;
    l) LOCAL_PORT="$OPTARG" ;;
    *) usage ;;
  esac
done

[ -z "$INSTANCE_ID" ] && usage
[ -z "$REMOTE_HOST" ] && usage
[ -z "$REGION" ] && { echo "error: -r <region> or AWS_REGION env var required"; exit 1; }

command -v aws >/dev/null || { echo "error: aws cli not found"; exit 1; }
command -v session-manager-plugin >/dev/null || {
  echo "error: session-manager-plugin not found — required by the AWS CLI for SSM sessions"
  echo "       https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"
  exit 1
}

echo "[info] forwarding localhost:${LOCAL_PORT} -> ${REMOTE_HOST}:${REMOTE_PORT} via ${INSTANCE_ID} (${REGION})"
echo "[info] point loadcannon at https://localhost:${LOCAL_PORT} with host_override=${REMOTE_HOST}"
echo "[info] ctrl-c to close the tunnel"

exec aws ssm start-session \
  --target "$INSTANCE_ID" \
  --region "$REGION" \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters "host=[\"${REMOTE_HOST}\"],portNumber=[\"${REMOTE_PORT}\"],localPortNumber=[\"${LOCAL_PORT}\"]"
