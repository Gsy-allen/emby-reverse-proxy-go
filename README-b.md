# emby-reverse-proxy-go 小白版操作流程

> 这份文档是给第一次部署的人看的。按顺序操作，不要跳步骤。

## 一、开始前先确认

你需要准备：

- 一台可以登录的服务器
- 一个自己的域名
- 能正常 ssh 连接服务器的终端

---

## 二、部署步骤

### 1）登录服务器

先登录到你的服务器。

### 2）如果还没安装 Docker，先安装 Docker

```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
```

### 3）准备工作目录

如果你**已经在使用 Nginx、Caddy、Nginx Proxy Manager 等反向代理工具**，进入它们所在的目录即可。接下来直接跳到6)。  

如果你**还没有任何反向代理环境**，可以先创建一个新目录并进入：

```bash
mkdir emby-proxy && cd emby-proxy
```

### 4）下载 `docker-compose.yml`

如果你当前目录里**还没有现成的 compose 文件**，执行下面命令下载：

```bash
wget -O docker-compose.yml https://raw.githubusercontent.com/Gsy-allen/emby-reverse-proxy-go/master/docker-compose.yml
```

如果你已经有自己的 compose 文件，这一步可以跳过。

### 5）修改数据库配置

打开 `docker-compose.yml`，把下面这些内容改成你自己的：

```yaml
services:
  app:
    environment:
      DB_MYSQL_USER: '改成你自己的用户名'
      DB_MYSQL_PASSWORD: '改成强密码'
      DB_MYSQL_NAME: '改成你自己的数据库名'

  db:
    environment:
      MYSQL_ROOT_PASSWORD: '改成强密码'
      MYSQL_DATABASE: '改成和上面一致的数据库名'
      MYSQL_USER: '改成和上面一致的用户名'
      MYSQL_PASSWORD: '改成和上面一致的强密码'
```

注意：

- `DB_MYSQL_NAME` 和 `MYSQL_DATABASE` 必须一致
- `DB_MYSQL_USER` 和 `MYSQL_USER` 必须一致
- `DB_MYSQL_PASSWORD` 和 `MYSQL_PASSWORD` 必须一致
- 建议所有密码都设置为强密码

下一步到7)。

### 6）如果你已经有自己的反向代理环境

如果你原本就有 Nginx / Caddy / Nginx Proxy Manager 的 compose 环境，那么不一定要重新下载完整文件。  
你只需要把 `emby-proxy` 服务加进你现有的 `docker-compose.yml`，并确保它和你的代理服务在同一网络。

另外，务必把 `emby-proxy` 加到 `depends_on`，否则服务启动时可能找不到它。

参考示例：

```yaml
services:
  app:
    depends_on:
      - emby-proxy

  emby-proxy:
    image: ghcr.io/gsy-allen/emby-proxy-go:v1.2.3
    container_name: emby-proxy
    restart: unless-stopped
    logging:
      driver: 'local'
      options:
        max-size: '10m'
        max-file: '5'
    environment:
      LISTEN_ADDR: ':8080'
      BLOCK_PRIVATE_TARGETS: 'true'
```

### 7）启动服务

```bash
docker compose up -d
```

---

## 三、配置 Nginx Proxy Manager

### 1）进入管理页面

通过下面地址访问 Nginx Proxy Manager：

```text
http://你的服务器IP:81
```

首次进入时，先创建管理员账号。

### 2）添加证书

进入证书列表，添加证书。  
如果你不会这一步，可以自行搜索 Nginx Proxy Manager 的证书申请教程。

![](screenshots/npm-ssl.png)

### 3）先反代 Nginx Proxy Manager 自己

进入主机列表，先把 Nginx Proxy Manager 自己代理出来，方便以后直接用域名访问管理页面。

![](screenshots/npm-1.png)

这里需要注意：

- 把 `npm.example.com` 改成你自己的域名
- 把 `172.18.0.1` 改成你的 Docker 网关地址
- 如果你是新安装环境，`172.18.0.1` 通常不用改 

SSL 相关选项按你的实际情况勾选，然后保存。

保存后，你就可以通过域名访问 Nginx Proxy Manager，之后请使用 https 域名形式访问。
由于首次创建账号时走的是 HTTP 明文，建议配置完成后在下一次使用域名访问时顺手修改一下密码。

### 4）反代 `emby-proxy` 服务

与上一步一样，建立一个新的代理用来代理本服务。

![](screenshots/npm-2.png)

这里把 `proxy.example.com` 改成你自己的域名。前往 SSL 页面，同样按你的实际情况勾选。

然后前往设置页，添加对应属性后保存：

`自定义 Nginx 配置` 中填写：

1) 使用默认443端口

```nginx
proxy_buffering off;
proxy_request_buffering off;
proxy_max_temp_file_size 0;
```

![](screenshots/npm-3.png)

2) 使用了其他端口，如8443等，则需要写全，否则读取不到端口信息

```nginx
location / {
    proxy_pass http://emby-proxy:8080;

    # 同443端口配置
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_max_temp_file_size 0;

    # 必须传递域名信息，否则无法识别端口
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Host $http_host;
    proxy_set_header X-Forwarded-Port $server_port;
    
    # websocket支持
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

![](screenshots/npm-4.png)

到这里，基础部署就完成了。

---

## 四、如何使用

以小幻和 senplayer 为例。

填写地址时，使用下面这种格式：

```text
https://{你的反代域名}:443/{emby服务器协议}/{emby服务器地址}/{emby服务器端口}
```

例如：

```text
https://proxy.example.com/https/example.com/8096
```

说明：

- `{你的反代域名}`：你刚才配置的反代域名
- `{emby服务器协议}`：填写 `http` 或 `https`
- `{emby服务器地址}`：目标 Emby 服务器地址
- `{emby服务器端口}`：目标 Emby 服务器端口
- `:443` 一般可以省略，不用专门写出来

使用本项目后，不需要手动区分推流地址，后端会自动识别并进行反代。

### 小幻示例

![](screenshots/xiaohuan.png)

### senplayer 示例

<img src="screenshots/senplayer.png" alt="senplayer 示例" width="420" />

---

## 五、常见提醒

- 改完配置后，如果服务没有生效，先重新执行一次 `docker compose up -d`
- 域名无法访问时，先检查域名解析是否指向你的服务器
- HTTPS 无法使用时，优先检查证书是否申请成功
- 如果是已有代理环境，重点检查是否和 `emby-proxy` 在同一网络