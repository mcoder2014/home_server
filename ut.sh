#!/bin/bash

TMP_DIR=/tmp/home_server/config

mkdir -p $TMP_DIR
cp config/config.yaml $TMP_DIR/config.yaml

export TEST_CONFIG_PATH=$TMP_DIR/config.yaml
go test -v ./...