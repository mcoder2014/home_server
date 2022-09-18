# Readme home server

本项目仅用于家庭服务器的 HTTP 服务，仅用于个人学习测试，非本人授权，不得用于商用或其他任何用途。

## 计划结构

- 设备客户端：用于在家庭硬件设备上运行，提供部分运维能力（上报系统负载、watch dog）
- 服务端：系统主体部分
- 前端：提供管理员用户界面

## 计划能力

- DDNS: 用于动态 dns 注册，由于小米路由器尚未具备 ipv6 的 ddns 能力，home server 可以为家庭硬件设备提供 ddns 能力；
- Watch Dog: 或者叫 heart beat，用于记录设备心跳数据，包含一些辅助数据，可以快速发现设备是否掉线；
- 家庭图书管理能力: 以 isbn 为基础，快速管理家庭中的纸质书及电子书；

## 安装说明


### home_client
1. 运行 `./build.sh`，编译程序;
2. 仿照 `client/config_example.yaml` 编写一份自己的配置文件；
3. 将二进制文件复制到指定文件夹 `sudo cp/bin/home_client /usr/local/bin/home_client`;
4. 将配置文件放在指定文件 `/etc/home_server/client_config.yaml`；
5. 将 systemd 配置文件复制到指定路径 `sudo cp script/systemd/home_client.service /etc/systemd/system`
6. 启动 `sudo systemctl start home_client.service`

#### 查看日志

```shell
sudo journalctl --unit home_client.service
```

## 版权信息 MIT LICENSE

本项目为个人兴趣，目的在于满足个人需求，不提供技术支持，使用本系统造成数据丢失、机器损坏等损失概不负责。