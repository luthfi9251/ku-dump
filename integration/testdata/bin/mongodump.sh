#!/bin/sh
exec docker run --rm --network host -i mongo:7 mongodump "$@"
