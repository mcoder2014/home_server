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
- WebDAV: 使用同一套账号体系，通过 WebDAV 协议实现外部文档访问，地址为 [https://www.server.com:port/webdav](https://www.server.com:port/webdav)；

## 安装说明

### 前端页面

编译前端文件，将 dist 目录下的文件拷贝到服务器的 /var/www/html 目录下，即可访问。

```shell
npm run build
```

### home_server

执行 `./build.sh` 构建 server 和 client 的二进制文件。

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

## FAQ

##### Q: 为什么服务没有使用 SSL?

A: 本项目是家庭服务，内网只暴露了 http，外网通过一个虚拟机的 nginx 反向代理实现，nginx 处配置了 SSL。其实服务就可以写的简化些，不需要使用 SSL。

反向代理的配置文件可以参考`config/nginx/home_server_backend.conf`。

## 版权信息 MIT LICENSE

本项目为个人兴趣，目的在于满足个人需求，不提供技术支持，使用本系统造成数据丢失、机器损坏等损失概不负责。