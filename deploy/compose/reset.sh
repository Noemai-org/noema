#!/bin/sh
# Destroys the LOCAL Compose project: containers, networks and volumes. Refuses any other project name.
set -eu
local_project="noema-local"
project="${COMPOSE_PROJECT_NAME:-$local_project}"
if [ "$project" != "$local_project" ]; then
  echo "reset refused: COMPOSE_PROJECT_NAME is '$project', not '$local_project'" >&2
  exit 1
fi
here="$(cd "$(dirname "$0")" && pwd)"
compose="docker compose -f $here/docker-compose.yml --profile dev --profile netcheck"
echo "reset will destroy, for project '$local_project':"
echo "  containers:"; $compose ps -a --format '    {{.Name}} ({{.State}})' || true
echo "  volumes:";    docker volume ls --filter "label=com.docker.compose.project=$local_project" --format '    {{.Name}}'
echo "  networks:";   docker network ls --filter "label=com.docker.compose.project=$local_project" --format '    {{.Name}}'
$compose down --volumes --remove-orphans
echo "reset done"
