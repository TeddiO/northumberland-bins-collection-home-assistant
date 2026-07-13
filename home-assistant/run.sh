#!/usr/bin/with-contenv sh

set -eu

UPRN="$(bashio::config 'uprn')"

if [ -z "${UPRN}" ]; then
    bashio::log.fatal "UPRN is required"
fi

export UPRN

exec /usr/bin/northumberland-bins