#!/bin/bash

SERVER_NAME="home_server"

mkdir -p output/bin
mkdir -p output/testdata

cp -r script/* output
cp config/config.yaml output

chmod a+x output/boot_server.sh

# 构建程序
go build -v -o output/bin/$SERVER_NAME ./
