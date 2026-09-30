#!/bin/bash
# End-to-end lab session timing through the API, the way a user starts a lab:
# POST /api/v1/lab-sessions -> LabSession -> controller -> pod -> VM ready -> API says running,
# then DELETE -> pod, LabSession and the session's SSH key Secret gone.
#
#   scripts/e2e-timing.sh up     # Postgres + Redis in Docker, schema, RabbitMQ port-forward, API
#   scripts/e2e-timing.sh run [vm|k8s|all]   # per lab: time create -> running and delete -> gone
#   scripts/e2e-timing.sh down   # stop the API and port-forward, remove the containers
#
# Needs docker, go, kubectl, jq and curl, and a cluster with the dozlab controller and RabbitMQ
# (namespace dozlab, Secret rabbitmq-credentials). Uses $KUBECONFIG like kubectl does.
# Each lab is a published row with its own init image (labs.init_image, which the API passes to
# the controller as spec.customImages.initImage): E2E_IMAGE_VM / E2E_IMAGE_K8S, default
# dozlab-init:local and dozman99/dozlab-init-k8s:local. The image must already be on the node.
# Each stage becomes one JSON line in $TIMINGS, in the same format as dozlab.sh's timings:
#   {"run":"<id>","step":"<stage>","start":<epoch>,"end":<epoch>,"seconds":<n>,"status":"ok|failed"}
set -euo pipefail

STATE="${E2E_STATE:-$HOME/.dozlab-e2e}"   # passwords, pids, logs; outside the repo
TIMINGS="${TIMINGS:-$([[ -d $HOME/.dozlab-local ]] && echo "$HOME/.dozlab-local/timings.jsonl" || echo "$STATE/timings.jsonl")}"
API_PORT="${API_PORT:-18080}"
PG_PORT="${PG_PORT:-55432}" REDIS_PORT="${REDIS_PORT:-56379}" RABBIT_PORT="${RABBIT_PORT:-55672}"
NS=default                 # the API creates LabSessions (and so lab pods) in "default"
STAGE_TIMEOUT="${STAGE_TIMEOUT:-300}"   # seconds per stage before the run fails
API="http://127.0.0.1:$API_PORT/api/v1"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$STATE"

say()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
die()  { printf '\033[1;31mxx %s\033[0m\n' "$*" >&2; exit 1; }
now()  { date +%s.%N; }
record() {  # record <step> <start> <end> <status>; prints the stage time
  local s="${1//\"/\\\"}"
  awk -v run="$RUN_ID" -v step="$s" -v a="$2" -v b="$3" -v st="$4" 'BEGIN {
    printf "{\"run\":\"%s\",\"step\":\"%s\",\"start\":%.3f,\"end\":%.3f,\"seconds\":%.2f,\"status\":\"%s\"}\n",
      run, step, a, b, (b > a ? b - a : 0), st }' >> "$TIMINGS"
  awk -v a="$2" -v b="$3" -v st="$4" -v s="$1" 'BEGIN { printf "  %8.2fs  %-6s %s\n", b - a, st, s }'
}
summary() {
  printf '\nTimings (%s, saved to %s):\n' "$RUN_ID" "$TIMINGS"
  grep -F "\"run\":\"$RUN_ID\"" "$TIMINGS" | jq -r '[.seconds, .status, .step] | @tsv' |
    awk -F'\t' '{ printf "  %8.2fs  %-6s %s\n", $1, $2, $3 }'
}
load_env() { [[ -f "$STATE/env" ]] || die "run '$0 up' first"; set -a; source "$STATE/env"; set +a; }
alive() { [[ -f "$STATE/$1.pid" ]] && kill -0 "$(cat "$STATE/$1.pid")" 2>/dev/null; }

# ---------------------------------------------------------------------------------------------
up() {
  for c in docker go kubectl jq curl; do command -v $c >/dev/null || die "$c not found"; done
  RUN_ID="$(date +%Y%m%dT%H%M%S)-e2e-up"
  if [[ ! -f "$STATE/env" ]]; then   # generated once and reused; never printed
    { echo "PG_PASSWORD=$(openssl rand -hex 16)"; echo "JWT_SECRET=$(openssl rand -hex 32)"
      echo "E2E_PASSWORD=$(openssl rand -hex 12)"; } > "$STATE/env"
    chmod 600 "$STATE/env"
  fi
  load_env
  local t

  say "Postgres + Redis (Docker)"; t=$(now)
  docker rm -f dozlab-e2e-pg dozlab-e2e-redis >/dev/null 2>&1 || true
  docker run -d --name dozlab-e2e-pg -e POSTGRES_PASSWORD="$PG_PASSWORD" -e POSTGRES_DB=dozlab \
    -p 127.0.0.1:$PG_PORT:5432 postgres:16-alpine >/dev/null
  docker run -d --name dozlab-e2e-redis -p 127.0.0.1:$REDIS_PORT:6379 redis:7-alpine >/dev/null
  # Over TCP: on first start the image runs a temporary server on the Unix socket only, before
  # it creates the "dozlab" database; that one would pass a socket check too early.
  for _ in $(seq 60); do docker exec dozlab-e2e-pg pg_isready -q -h 127.0.0.1 -U postgres -d dozlab && break; sleep 0.5; done
  for f in "$REPO"/internal/database/migrations/*.up.sql; do
    docker exec -i dozlab-e2e-pg psql -q -v ON_ERROR_STOP=1 -U postgres -d dozlab < "$f" >/dev/null
  done
  record "up: postgres + redis + schema" "$t" "$(now)" ok

  say "RabbitMQ port-forward (cluster, namespace dozlab)"; t=$(now)
  alive portforward && kill "$(cat "$STATE/portforward.pid")" || true
  kubectl -n dozlab port-forward svc/rabbitmq "$RABBIT_PORT:5672" > "$STATE/portforward.log" 2>&1 &
  echo $! > "$STATE/portforward.pid"
  for _ in $(seq 40); do (exec 3<>/dev/tcp/127.0.0.1/$RABBIT_PORT) 2>/dev/null && break; sleep 0.25; done
  record "up: rabbitmq port-forward" "$t" "$(now)" ok

  say "API (go build ./cmd/api, then run on :$API_PORT)"; t=$(now)
  (cd "$REPO" && go build -o "$STATE/api" ./cmd/api)
  alive api && kill "$(cat "$STATE/api.pid")" || true
  local ru rp
  ru=$(kubectl -n dozlab get secret rabbitmq-credentials -o jsonpath='{.data.username}' | base64 -d)
  rp=$(kubectl -n dozlab get secret rabbitmq-credentials -o jsonpath='{.data.password}' | base64 -d)
  PORT=$API_PORT JWT_SECRET="$JWT_SECRET" \
    DB_HOST=127.0.0.1 DB_PORT=$PG_PORT DB_NAME=dozlab DB_USER=postgres DB_PASSWORD="$PG_PASSWORD" \
    REDIS_HOST=127.0.0.1 REDIS_PORT=$REDIS_PORT \
    RABBITMQ_URL="amqp://$ru:$rp@127.0.0.1:$RABBIT_PORT/" \
    nohup "$STATE/api" > "$STATE/api.log" 2>&1 &
  echo $! > "$STATE/api.pid"
  for _ in $(seq 120); do curl -fsS "http://127.0.0.1:$API_PORT/health" >/dev/null 2>&1 && break
    alive api || die "API exited; see $STATE/api.log"; sleep 0.5; done
  curl -fsS "http://127.0.0.1:$API_PORT/health" >/dev/null || die "API not healthy; see $STATE/api.log"
  record "up: api build + start" "$t" "$(now)" ok
  summary
}

down() {
  for p in api portforward; do alive $p && kill "$(cat "$STATE/$p.pid")" || true; rm -f "$STATE/$p.pid"; done
  docker rm -f dozlab-e2e-pg dozlab-e2e-redis >/dev/null 2>&1 || true
  echo "stopped (credentials kept in $STATE/env)"
}

# ---------------------------------------------------------------------------------------------
psql_q() { docker exec -i dozlab-e2e-pg psql -qtA -v ON_ERROR_STOP=1 -U postgres -d dozlab -c "$1"; }
api() {  # api <method> <path> [json]; prints the body, fails on HTTP >= 400
  local out code
  out=$(curl -sS -w '\n%{http_code}' -X "$1" "$API$2" -H 'Content-Type: application/json' \
    ${TOKEN:+-H "Authorization: Bearer $TOKEN"} ${3:+-d "$3"})
  code=${out##*$'\n'}; out=${out%$'\n'*}
  [[ $code -lt 400 ]] || { echo "$1 $2 -> HTTP $code: $out" >&2; return 1; }
  printf '%s' "$out"
}

# wait_for <step> <check-command...>: polls until the check succeeds, records the stage from the
# previous milestone ($MARK) to now, and moves $MARK. Fails the run after $STAGE_TIMEOUT.
wait_for() {
  local step=$1; shift
  local end=$((SECONDS + STAGE_TIMEOUT))
  until "$@" 2>/dev/null; do
    ((SECONDS < end)) || { record "$step" "$MARK" "$(now)" failed; FAILED="$step"; return 1; }
    sleep 0.2
  done
  local t; t=$(now); record "$step" "$MARK" "$t" ok; MARK=$t
}
pod_init_image()  { kubectl -n $NS get pod "lab-session-$SESSION" -o jsonpath='{.spec.initContainers[?(@.name=="init-rootfs")].image}'; }
pod_json()        { kubectl -n $NS get pod "lab-session-$SESSION" -o json; }
labsession_exists() { kubectl -n $NS get labsession "session-$SESSION" >/dev/null; }
pod_exists()      { kubectl -n $NS get pod "lab-session-$SESSION" >/dev/null; }
pod_scheduled()   { pod_json | jq -e '.status.conditions[]? | select(.type=="PodScheduled" and .status=="True")' >/dev/null; }
rootfs_done()     { pod_json | jq -e '.status.initContainerStatuses[]? | select(.name=="init-rootfs") | .state.terminated.exitCode == 0' >/dev/null; }
vm_ready()        { pod_json | jq -e '.status.containerStatuses[]? | select(.name=="firecracker-vm") | .ready' >/dev/null; }
pod_ready()       { pod_json | jq -e '.status.conditions[]? | select(.type=="Ready" and .status=="True")' >/dev/null; }
# What a client polling the API sees. (The API's own session.status stays "pending": phase events
# only reach WebSocket clients and don't update the database.)
api_running()     { api GET "/lab-sessions/$SESSION" | jq -e '.k8s_status.phase == "Running"' >/dev/null; }
pod_gone()        { ! kubectl -n $NS get pod "lab-session-$SESSION" >/dev/null 2>&1; }
labsession_gone() { ! kubectl -n $NS get labsession "session-$SESSION" >/dev/null 2>&1; }
secret_gone()     { ! kubectl -n $NS get secret "lab-session-$SESSION-ssh" >/dev/null 2>&1; }

cleanup_session() {  # on failure or interrupt: don't leave a session behind
  [[ -n "${SESSION:-}" && -z "${DELETED:-}" ]] || return 0
  echo "cleaning up session $SESSION"
  api DELETE "/lab-sessions/$SESSION" >/dev/null 2>&1 || kubectl -n $NS delete labsession "session-$SESSION" --ignore-not-found >/dev/null 2>&1 || true
}

lab_image() { case $1 in vm) echo "${E2E_IMAGE_VM:-dozlab-init:local}" ;; k8s) echo "${E2E_IMAGE_K8S:-dozman99/dozlab-init-k8s:local}" ;; esac; }

run() {
  load_env
  curl -fsS "http://127.0.0.1:$API_PORT/health" >/dev/null 2>&1 || die "API isn't running; run '$0 up' first"
  local labs
  case "${1:-vm}" in all) labs="vm k8s" ;; vm|k8s) labs=$1 ;; *) die "usage: $0 run [vm|k8s|all]" ;; esac
  # Migrations run in 'up', before the API starts: changing a table under the running API breaks
  # its cached queries.
  [[ -n "$(psql_q "SELECT 1 FROM information_schema.columns WHERE table_name = 'labs' AND column_name = 'init_image'")" ]] ||
    die "the database predates labs.init_image; run '$0 up' again"
  api POST /auth/register "$(jq -nc --arg p "$E2E_PASSWORD" '{username:"e2e-runner", email:"e2e-runner@dozlab.test", password:$p}')" >/dev/null 2>&1 || true
  local ts; ts=$(date +%Y%m%dT%H%M%S)
  for lab in $labs; do run_lab "$lab" "$ts-e2e-$lab"; done
}

run_lab() {
  local lab=$1 image; image=$(lab_image "$1")
  RUN_ID=$2 SESSION="" DELETED=""
  trap cleanup_session EXIT

  say "$lab lab ($image)"
  LAB_ID=$(psql_q "INSERT INTO labs (name, slug, description, is_published, init_image)
    VALUES ('E2E $lab lab', 'e2e-$lab-lab', 'Lab session timing (scripts/e2e-timing.sh)', true, '$image')
    ON CONFLICT (slug) DO UPDATE SET is_published = true, init_image = EXCLUDED.init_image RETURNING id;")

  local t body
  t=$(now); TOKEN=$(api POST /auth/login "$(jq -nc --arg p "$E2E_PASSWORD" '{username:"e2e-runner", password:$p}')" | jq -r .tokens.access_token)
  record "api: login" "$t" "$(now)" ok
  [[ -n "$TOKEN" && "$TOKEN" != null ]] || die "login returned no token"

  local t0; t0=$(now); MARK=$t0
  body=$(api POST /lab-sessions/ "$(jq -nc --arg lab "$LAB_ID" \
    --arg m "${E2E_MEMORY:-}" --arg c "${E2E_CPU:-}" \
    '{lab_id:$lab} + (if $m != "" or $c != "" then {resources:{memory:$m, cpu:$c}} else {} end)')")
  SESSION=$(jq -r .session.id <<<"$body")
  t=$(now); record "api: POST lab-session" "$MARK" "$t" ok; MARK=$t
  echo "session $SESSION"

  FAILED=""
  wait_for "k8s: LabSession created"         labsession_exists &&
  wait_for "k8s: pod created"                pod_exists || true
  # The controller must boot this lab's rootfs; an older controller (or a CRD without
  # spec.customImages) silently uses its default image instead.
  local got; got=$(pod_init_image 2>/dev/null || true)
  if [[ -z "$FAILED" && "$got" != "$image" ]]; then
    record "k8s: pod runs the lab's init image" "$MARK" "$(now)" failed
    FAILED="wrong init image"
    echo "  init-rootfs runs '$got', not '$image': the controller ignored the lab's image" \
      "(needs dozlab-controller#10 and the CRD from dozlab-infra#7)"
  fi
  [[ -n "$FAILED" ]] || {
    wait_for "k8s: pod scheduled"              pod_scheduled &&
    wait_for "k8s: init-rootfs done"           rootfs_done &&
    wait_for "k8s: VM ready (sshd answers)"    vm_ready &&
    wait_for "k8s: all sidecars ready (pod Ready)" pod_ready &&
    wait_for "api: session reports Running"    api_running || true
  }
  if [[ -n "$FAILED" ]]; then
    record "TOTAL create -> running" "$t0" "$(now)" failed
    [[ "$FAILED" == "wrong init image" ]] || kubectl -n $NS describe pod "lab-session-$SESSION" 2>/dev/null | tail -15 || true
  else
    record "TOTAL create -> running" "$t0" "$MARK" ok
  fi

  say "$lab lab: delete -> gone"
  local had_secret=""; secret_gone || had_secret=1
  local d0; d0=$(now); MARK=$d0
  api DELETE "/lab-sessions/$SESSION" >/dev/null; DELETED=1
  t=$(now); record "api: DELETE lab-session" "$MARK" "$t" ok; MARK=$t
  FAILED=""
  wait_for "k8s: pod gone"        pod_gone &&
  wait_for "k8s: LabSession gone" labsession_gone &&
  { [[ -z "$had_secret" ]] || wait_for "k8s: SSH key Secret gone" secret_gone; } || true
  record "TOTAL delete -> gone" "$d0" "$MARK" "$([[ -z "$FAILED" ]] && echo ok || echo failed)"
  [[ -n "$had_secret" ]] || echo "  (no per-session SSH key Secret: the controller predates per-session keys)"

  summary
}

case "${1:-}" in
  up) up ;;
  run) run "${2:-vm}" ;;
  down) down ;;
  *) die "usage: $0 up|run [vm|k8s|all]|down" ;;
esac
