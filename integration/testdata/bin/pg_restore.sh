#!/bin/sh
exec docker run --rm --network host -i -e PGPASSWORD="$PGPASSWORD" postgres:16-alpine pg_restore "$@"
