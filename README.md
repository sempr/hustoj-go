# 如何替换原有判题内核

## 工作准备

1. 假设你已经使用脚本安装好了 https://github.com/zhblue/hustoj 这个项目
2. 假设你的机器上有安装好docker并且可以正常使用
3. 目前仅支持ubuntu24.04 其它系统没有做过测试

## 安装流程

1. clone 本项目到服务器任意目录
2. 安装golang `sudo apt-get install golang`
3. 在本项目的根目录下执行 `make` 编译
4. 把编译出来的 `hustoj-go` 复制到 `/usr/bin` 目录
5. 把 `./extra/judged-go.service` 文件复制到 `/etc/systemd/system/` 目录
6. 去 `./extra` 目录 开始准备rootfs 执行 `bash build_rootfs.sh <id>` 可以打包相应语言的rootfs，建议初始时编译 0(C) 1(C++) 2(Pascal) 3(Java) 四种语言 其它语言的打包方法类似
7. 把 `./extra/etc/langs/` 目录复制到 `/home/judge/etc/` 下面 如果rootfs调整了 需要更新相应的文件 比如C语言是 0.langs.toml
8. rootfs准备好 /home/judge/etc/langs 都准备好之后 就可以停掉原有的 judged 使用 `systemctl enable --now judged-go` 来启动新的内核来使用了

## 其它

使用过程中如果有疑问 请提Issue

