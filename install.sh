#!/bin/bash

# 1. install docker and docker mirror
apt-get install -y docker.io

cat > /etc/docker/daemon.json <<EOF
{
	"registry-mirrors": [
	        "https://docker.1ms.run",
	        "https://docker.xuanyuan.me"
    	],
	"live-restore": true,
	"log-opts": {
		"max-size": "512m",
		"max-file": "3"
	}
}
EOF

systemctl enable --now docker

# 2. use docker build hustoj-go
# 3. build docker images
# 4. copy configs
# 5. copy .service and enable
