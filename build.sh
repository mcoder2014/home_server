#!/bin/bash

PROGRAM_NAME="home_server"

mkdir -p output/bin
cp script/* output
chmod +x output/boot.sh

# 构建程序
go build -v -o output/bin/$PROGRAM_NAME