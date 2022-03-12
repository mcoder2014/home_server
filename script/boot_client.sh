#!/bin/bash

PROGRAM_NAME="home_client"
CURDIR=$(cd $(dirname $0); pwd)

export GIN_MODE=release

exec $CURDIR/bin/$PROGRAM_NAME