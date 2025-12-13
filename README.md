# toolspc
c and go tools


file tree print
simple 权限ignore 


$ ftree -p ~/projects -d 3 -n 100
📁 路径: /home/user/projects
📊 统计: 15 目录, 85 文件, 100 节点

└── 📁 projects
    ├── 📁 project1
    │   ├── 📄 README.md (1.2KB)
    │   ├── 📁 src
    │   │   ├── 📄 main.go (2.5KB)
    │   │   └── 📄 utils.go (1.8KB)
    │   └── 📁 docs
    ├── 📁 project2
    │   ├── 📁 src
    │   │   ├── 📁 lib
    │   │   └── 📁 tests
    │   └── 📄 Makefile
    └── 📁 personal
        └── 📁 notes

⚠️  节点数已达限制 (100)，已停止遍历
   使用 --max-nodes 参数调整限制m
