#!/bin/sh
set -eu
cd "$(dirname "$0")"
printf 'adapter spike ready\n' | cmp -s - message.txt
