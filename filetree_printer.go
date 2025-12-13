package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// FileNodeType 文件节点类型
type FileNodeType int

const (
	FileTypeRegular FileNodeType = iota
	FileTypeDirectory
	FileTypeSymlink
	FileTypeExecutable
	FileTypeHidden
)

// FileNode 文件节点
type FileNode struct {
	Name     string
	Path     string
	Type     FileNodeType
	Size     int64
	ModTime  time.Time
	Mode     os.FileMode
	Children []*FileNode
	Parent   *FileNode
	Depth    int
	IsLast   bool
}

// FileTreeConfig 文件树配置
type FileTreeConfig struct {
	MaxDepth     int
	MaxNodes     int
	ShowHidden   bool
	ShowSize     bool
	ShowTime     bool
	ShowMode     bool
	FollowLinks  bool
	SortByName   bool
	IgnoreList   []string
	OnlyDirs     bool
	OnlyFiles    bool
	Pattern      string
	HumanSize    bool
	CountOnly    bool
}

// DefaultFileTreeConfig 默认配置
func DefaultFileTreeConfig() *FileTreeConfig {
	return &FileTreeConfig{
		MaxDepth:    20,
		MaxNodes:    100,
		ShowHidden:  false,
		ShowSize:    false,
		ShowTime:    false,
		ShowMode:    false,
		FollowLinks: false,
		SortByName:  true,
		IgnoreList: []string{
			".git", ".svn", ".hg", ".DS_Store",
			"node_modules", "__pycache__", ".cache",
		},
		OnlyDirs:  false,
		OnlyFiles: false,
		HumanSize: true,
		CountOnly: false,
	}
}

// FileTree 文件树
type FileTree struct {
	Root    *FileNode
	Config  *FileTreeConfig
	nodeCount int
	dirCount  int
	fileCount int
	sizeTotal int64
}

// NewFileTree 创建文件树
func NewFileTree(config *FileTreeConfig) *FileTree {
	if config == nil {
		config = DefaultFileTreeConfig()
	}
	return &FileTree{
		Root:   nil,
		Config: config,
	}
}

// BuildFromPath 从路径构建文件树
func (ft *FileTree) BuildFromPath(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %v", err)
	}
	
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("无法访问路径: %v", err)
	}
	
	ft.Root = &FileNode{
		Name:     filepath.Base(path),
		Path:     path,
		Type:     FileTypeDirectory,
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		Mode:     info.Mode(),
		Children: []*FileNode{},
		Parent:   nil,
		Depth:    0,
		IsLast:   true,
	}
	
	ft.nodeCount = 1
	ft.dirCount = 1
	ft.fileCount = 0
	
	if info.IsDir() {
		return ft.buildDirectoryTree(ft.Root, 1)
	} else {
		ft.fileCount = 1
		return ft.parseElispFile(path)
	}
}

// buildDirectoryTree 递归构建目录树
func (ft *FileTree) buildDirectoryTree(node *FileNode, depth int) error {
	if depth > ft.Config.MaxDepth {
		return nil
	}
	
	entries, err := ioutil.ReadDir(node.Path)
	if err != nil {
		return err
	}
	
	// 过滤和排序
	filteredEntries := ft.filterEntries(entries, node.Path)
	
	for i, entry := range filteredEntries {
		// 检查节点数限制
		if ft.nodeCount >= ft.Config.MaxNodes {
			return fmt.Errorf("节点数超过限制 (%d), 已停止遍历", ft.Config.MaxNodes)
		}
		
		entryPath := filepath.Join(node.Path, entry.Name())
		var nodeType FileNodeType
		
		switch {
		case entry.IsDir():
			nodeType = FileTypeDirectory
			ft.dirCount++
		case entry.Mode()&os.ModeSymlink != 0:
			nodeType = FileTypeSymlink
			ft.fileCount++
		case entry.Mode()&0111 != 0:
			nodeType = FileTypeExecutable
			ft.fileCount++
		case strings.HasPrefix(entry.Name(), "."):
			nodeType = FileTypeHidden
			ft.fileCount++
		default:
			nodeType = FileTypeRegular
			ft.fileCount++
		}
		
		childNode := &FileNode{
			Name:     entry.Name(),
			Path:     entryPath,
			Type:     nodeType,
			Size:     entry.Size(),
			ModTime:  entry.ModTime(),
			Mode:     entry.Mode(),
			Children: []*FileNode{},
			Parent:   node,
			Depth:    depth,
			IsLast:   i == len(filteredEntries)-1,
		}
		
		node.Children = append(node.Children, childNode)
		ft.nodeCount++
		
		// 如果是目录，递归构建
		if entry.IsDir() && ft.Config.FollowLinks {
			ft.buildDirectoryTree(childNode, depth+1)
		}
	}
	
	return nil
}

// filterEntries 过滤条目
func (ft *FileTree) filterEntries(entries []os.FileInfo, parentPath string) []os.FileInfo {
	var result []os.FileInfo
	
	for _, entry := range entries {
		// 跳过隐藏文件（如果不显示）
		if !ft.Config.ShowHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		
		// 检查忽略列表
		skip := false
		for _, ignore := range ft.Config.IgnoreList {
			if entry.Name() == ignore {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		
		// 检查模式匹配
		if ft.Config.Pattern != "" {
			matched, _ := filepath.Match(ft.Config.Pattern, entry.Name())
			if !matched {
				continue
			}
		}
		
		// 检查只显示目录/文件
		if ft.Config.OnlyDirs && !entry.IsDir() {
			continue
		}
		if ft.Config.OnlyFiles && entry.IsDir() {
			continue
		}
		
		result = append(result, entry)
	}
	
	return result
}

// parseElispFile 解析Elisp文件
func (ft *FileTree) parseElispFile(filepath string) error {
	content, err := ioutil.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %v", err)
	}
	
	// 简单解析Elisp文件，提取树结构
	// 这里假设Elisp文件定义了树结构
	lines := strings.Split(string(content), "\n")
	
	// 这里可以添加更复杂的Elisp解析逻辑
	// 简化处理：将文件内容视为节点
	ft.Root.Children = []*FileNode{
		{
			Name:     "文件内容",
			Type:     FileTypeRegular,
			Size:     int64(len(content)),
			Children: []*FileNode{},
		},
	}
	
	return nil
}

// Print 打印文件树
func (ft *FileTree) Print() {
	if ft.Config.CountOnly {
		ft.printCounts()
		return
	}
	
	if ft.Root == nil {
		fmt.Println("树为空")
		return
	}
	
	// 打印摘要
	ft.printSummary()
	fmt.Println()
	
	// 打印树结构
	ft.printNode(ft.Root, "", true)
	
	// 如果被中断，打印提示
	if ft.nodeCount >= ft.Config.MaxNodes {
		fmt.Printf("\n⚠️  节点数已达限制 (%d)，已停止遍历\n", ft.Config.MaxNodes)
		fmt.Printf("   使用 --max-nodes 参数调整限制\n")
	}
}

// printNode 递归打印节点
func (ft *FileTree) printNode(node *FileNode, prefix string, isLast bool) {
	// 构建当前行的前缀
	linePrefix := prefix
	if prefix != "" {
		if isLast {
			linePrefix += "└── "
		} else {
			linePrefix += "├── "
		}
	}
	
	// 构建节点显示文本
	nodeText := ft.formatNodeText(node)
	
	// 打印当前节点
	fmt.Printf("%s%s\n", linePrefix, nodeText)
	
	// 构建子节点前缀
	childPrefix := prefix
	if prefix != "" {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}
	
	// 递归打印子节点
	for i, child := range node.Children {
		isLastChild := i == len(node.Children)-1
		ft.printNode(child, childPrefix, isLastChild)
	}
}

// formatNodeText 格式化节点文本
func (ft *FileTree) formatNodeText(node *FileNode) string {
	var parts []string
	
	// 添加图标
	switch node.Type {
	case FileTypeDirectory:
		parts = append(parts, "📁")
	case FileTypeSymlink:
		parts = append(parts, "🔗")
	case FileTypeExecutable:
		parts = append(parts, "⚡")
	case FileTypeHidden:
		parts = append(parts, "👁️")
	default:
		parts = append(parts, "📄")
	}
	
	// 添加名称
	name := node.Name
	if node.Type == FileTypeDirectory {
		name = "\033[1;34m" + name + "\033[0m" // 蓝色
	} else if node.Type == FileTypeExecutable {
		name = "\033[1;32m" + name + "\033[0m" // 绿色
	} else if node.Type == FileTypeSymlink {
		name = "\033[1;36m" + name + "\033[0m" // 青色
	}
	parts = append(parts, name)
	
	// 添加额外信息
	if ft.Config.ShowMode {
		parts = append(parts, fmt.Sprintf("[%s]", node.Mode.String()))
	}
	
	if ft.Config.ShowSize {
		sizeStr := ft.formatSize(node.Size)
		parts = append(parts, fmt.Sprintf("(%s)", sizeStr))
	}
	
	if ft.Config.ShowTime {
		timeStr := node.ModTime.Format("2006-01-02 15:04")
		parts = append(parts, fmt.Sprintf("@%s", timeStr))
	}
	
	return strings.Join(parts, " ")
}

// formatSize 格式化大小
func (ft *FileTree) formatSize(bytes int64) string {
	if !ft.Config.HumanSize {
		return fmt.Sprintf("%d", bytes)
	}
	
	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(bytes)
	i := 0
	
	for size >= 1024 && i < len(sizes)-1 {
		size /= 1024
		i++
	}
	
	return fmt.Sprintf("%.1f%s", size, sizes[i])
}

// printSummary 打印摘要
func (ft *FileTree) printSummary() {
	fmt.Printf("📁 路径: %s\n", ft.Root.Path)
	fmt.Printf("📊 统计: %d 目录, %d 文件, %d 节点\n", 
		ft.dirCount, ft.fileCount, ft.nodeCount)
	
	if ft.sizeTotal > 0 {
		fmt.Printf("💾 总大小: %s\n", ft.formatSize(ft.sizeTotal))
	}
}

// printCounts 仅打印计数
func (ft *FileTree) printCounts() {
	fmt.Printf("目录: %d\n", ft.dirCount)
	fmt.Printf("文件: %d\n", ft.fileCount)
	fmt.Printf("总计: %d\n", ft.nodeCount)
	
	if ft.sizeTotal > 0 {
		fmt.Printf("大小: %s\n", ft.formatSize(ft.sizeTotal))
	}
}

// FileTreePrinter 文件树打印器
type FileTreePrinter struct {
	config *FileTreeConfig
}

// NewFileTreePrinter 创建文件树打印器
func NewFileTreePrinter(config *FileTreeConfig) *FileTreePrinter {
	return &FileTreePrinter{config: config}
}

// PrintPath 打印路径
func (p *FileTreePrinter) PrintPath(path string) error {
	tree := NewFileTree(p.config)
	
	// 检查路径是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("路径不存在: %s", path)
	}
	
	// 构建树
	err := tree.BuildFromPath(path)
	if err != nil {
		return fmt.Errorf("构建树失败: %v", err)
	}
	
	// 打印树
	tree.Print()
	return nil
}


二、命令行工具

// main.go
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	// 命令行参数
	var (
		path       string
		maxDepth   int
		maxNodes   int
		showHidden bool
		showSize   bool
		showTime   bool
		showMode   bool
		followLinks bool
		noSort     bool
		onlyDirs   bool
		onlyFiles  bool
		pattern    string
		humanSize  bool
		countOnly  bool
		help       bool
		version    bool
	)
	
	flag.StringVar(&path, "p", ".", "要扫描的路径")
	flag.IntVar(&maxDepth, "d", 20, "最大深度")
	flag.IntVar(&maxNodes, "n", 100, "最大节点数")
	flag.BoolVar(&showHidden, "a", false, "显示隐藏文件")
	flag.BoolVar(&showSize, "s", false, "显示文件大小")
	flag.BoolVar(&showTime, "t", false, "显示修改时间")
	flag.BoolVar(&showMode, "m", false, "显示文件权限")
	flag.BoolVar(&followLinks, "L", false, "跟随符号链接")
	flag.BoolVar(&noSort, "U", false, "不排序（默认按名称排序）")
	flag.BoolVar(&onlyDirs, "D", false, "只显示目录")
	flag.BoolVar(&onlyFiles, "F", false, "只显示文件")
	flag.StringVar(&pattern, "P", "", "文件模式匹配（如 *.go）")
	flag.BoolVar(&humanSize, "H", true, "人类可读的文件大小")
	flag.BoolVar(&countOnly, "c", false, "仅显示计数")
	flag.BoolVar(&help, "h", false, "显示帮助")
	flag.BoolVar(&version, "v", false, "显示版本")
	
	// 自定义用法说明
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "文件树打印工具 v1.0\n")
		fmt.Fprintf(os.Stderr, "用法: %s [选项] [路径]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n示例:\n")
		fmt.Fprintf(os.Stderr, "  %s -p . -a -s          # 显示当前目录（包含隐藏文件）\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -p /usr -d 3       # 显示/usr目录，深度3\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -P \"*.go\"         # 只显示Go文件\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -c                 # 仅显示文件计数\n", os.Args[0])
	}
	
	flag.Parse()
	
	// 处理帮助和版本
	if help {
		flag.Usage()
		return
	}
	
	if version {
		fmt.Println("文件树打印工具 v1.0")
		return
	}
	
	// 如果有额外的参数，使用第一个作为路径
	if len(flag.Args()) > 0 {
		path = flag.Args()[0]
	}
	
	// 创建配置
	config := DefaultFileTreeConfig()
	config.MaxDepth = maxDepth
	config.MaxNodes = maxNodes
	config.ShowHidden = showHidden
	config.ShowSize = showSize
	config.ShowTime = showTime
	config.ShowMode = showMode
	config.FollowLinks = followLinks
	config.SortByName = !noSort
	config.OnlyDirs = onlyDirs
	config.OnlyFiles = onlyFiles
	config.Pattern = pattern
	config.HumanSize = humanSize
	config.CountOnly = countOnly
	
	// 创建打印器
	printer := NewFileTreePrinter(config)
	
	// 打印路径
	err := printer.PrintPath(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}