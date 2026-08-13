// 静态 CLI 词库（Linux / DevOps 高频命令），供 Trie 前缀补全。
export const STATIC_DICT: string[] = [
  // shell / 导航
  'ls', 'll', 'la', 'cd', 'pwd', 'pushd', 'popd', 'dirs', 'tree', 'find', 'locate', 'which', 'whereis', 'type',
  'mkdir', 'rmdir', 'rm', 'cp', 'mv', 'ln', 'touch', 'chmod', 'chown', 'chgrp', 'stat', 'file', 'du', 'df', 'mount', 'umount',
  'cat', 'tac', 'head', 'tail', 'less', 'more', 'nano', 'vim', 'vi', 'emacs', 'sed', 'awk', 'grep', 'egrep', 'fgrep', 'rg', 'ag',
  'sort', 'uniq', 'cut', 'tr', 'wc', 'diff', 'patch', 'tee', 'xargs', 'parallel',
  // 压缩
  'tar', 'gzip', 'gunzip', 'bzip2', 'xz', 'zip', 'unzip', 'zcat', 'zless',
  // 进程 / 系统
  'ps', 'top', 'htop', 'btop', 'kill', 'killall', 'pkill', 'pgrep', 'nice', 'renice', 'nohup', 'jobs', 'fg', 'bg', 'disown',
  'free', 'uptime', 'uname', 'hostname', 'hostnamectl', 'lscpu', 'lsmem', 'lsblk', 'lspci', 'lsusb', 'dmesg', 'journalctl',
  'systemctl', 'service', 'timedatectl', 'localectl', 'loginctl',
  // 网络
  'ping', 'ping6', 'traceroute', 'mtr', 'dig', 'nslookup', 'host', 'curl', 'wget', 'http', 'ssh', 'scp', 'sftp', 'rsync',
  'nc', 'ncat', 'netcat', 'telnet', 'nmap', 'ss', 'netstat', 'ip', 'ifconfig', 'iptables', 'nft', 'firewall-cmd',
  'tcpdump', 'wireshark', 'traceroute6', 'arp', 'route', 'resolvectl',
  // 包管理
  'apt', 'apt-get', 'aptitude', 'dpkg', 'yum', 'dnf', 'rpm', 'pacman', 'zypper', 'apk', 'brew', 'snap', 'flatpak',
  'pip', 'pip3', 'npm', 'npx', 'yarn', 'pnpm', 'cargo', 'go', 'gem', 'composer',
  // docker / k8s / 容器
  'docker', 'docker compose', 'docker-compose', 'podman', 'nerdctl', 'kubectl', 'helm', 'k9s', 'minikube', 'kind',
  'ctr', 'crictl', 'buildah', 'skopeo',
  // git
  'git', 'git status', 'git add', 'git commit', 'git push', 'git pull', 'git fetch', 'git clone', 'git checkout',
  'git switch', 'git branch', 'git merge', 'git rebase', 'git cherry-pick', 'git stash', 'git log', 'git diff',
  'git reset', 'git revert', 'git remote', 'git tag', 'git show', 'git blame', 'git config',
  // 文本 / 编辑
  'echo', 'printf', 'read', 'export', 'env', 'printenv', 'set', 'unset', 'alias', 'unalias', 'history', 'clear', 'reset',
  'date', 'cal', 'bc', 'expr', 'test', 'true', 'false', 'yes', 'sleep', 'timeout', 'watch', 'time',
  // 用户 / 权限
  'sudo', 'su', 'passwd', 'useradd', 'userdel', 'usermod', 'groupadd', 'id', 'whoami', 'who', 'w', 'last', 'lastlog',
  // 磁盘 / 存储
  'fdisk', 'parted', 'mkfs', 'fsck', 'tune2fs', 'resize2fs', 'lvm', 'pvdisplay', 'vgdisplay', 'lvdisplay',
  'mdadm', 'cryptsetup', 'dd', 'sync',
  // 数据库
  'mysql', 'mysqldump', 'psql', 'pg_dump', 'redis-cli', 'mongo', 'mongosh', 'sqlite3', 'clickhouse-client',
  // 运维脚本
  'crontab', 'at', 'batch', 'systemd-run', 'tmux', 'screen', 'byobu', 'script', 'strace', 'lsof', 'iotop', 'iftop',
  'vmstat', 'iostat', 'sar', 'mpstat', 'pidstat', 'perf',
  // 云 / IaC
  'aws', 'gcloud', 'az', 'terraform', 'ansible', 'ansible-playbook', 'vagrant', 'packer',
  // 传输协议相关
  'rz', 'sz', 'lrz', 'lsz', 'trz', 'tsz',
  // make / 构建
  'make', 'cmake', 'ninja', 'gcc', 'g++', 'clang', 'ld', 'ar', 'nm', 'objdump', 'strip', 'node', 'python', 'python3',
  'ruby', 'perl', 'php', 'java', 'javac', 'mvn', 'gradle', 'bazel',
]

export const STATIC_DICT_SET = new Set(STATIC_DICT)
