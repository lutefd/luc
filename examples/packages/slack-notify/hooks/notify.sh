#!/bin/sh
IFS= read -r request
printf '%s\n' "slack-notify received: $request" >&2
printf '%s\n' '{"type":"done","done":true}'
