#!/bin/bash

SERVER_NAME="home_server"
CLIENT_NAME="home_client"

mkdir -p output/bin
cp script/* output
cp config/config.yaml output

chmod a+x output/boot_server.sh
chmod a+x output/boot_client.sh

# 构建程序
go build -v -o output/bin/$SERVER_NAME ./
go build -v -o output/bin/$CLIENT_NAME ./clients