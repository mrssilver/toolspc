
//check advanced read文件/目录读取、权限检查、树形打印和Elisp解析。


package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// ==================== 常量定义 ====================

const (
	version = "3.0.0"
	author  = "FileTree Printer"
)

// 文件节点类型
type FileNodeType int

const (
	FileTypeRegular FileNodeType = iota
	FileTypeDirectory
	FileTypeSymlink
	FileTypeExecutable
	FileTypeHidden
	FileTypeElisp
	FileTypePermissionDenied
)

// ==================== 数据结构定义 ====================

// FileNode 文件节点
type FileNode struct {
	Name     string
	Path     string
	Type     FileNodeType
	Size     int64
	ModTime  time.Time
	Mode     os.FileMode
	Children []*FileNode
	Depth    int
	IsLast   bool
	Error    string
	Owner    string
	Group    string
	Perm     string
}

// FileTreeConfig 文件树配置
type FileTreeConfig struct {
	MaxDepth     int
	MaxNodes     int
	ShowHidden   bool
	ShowSize     bool
	ShowTime     bool
	ShowMode     bool
	ShowOwner    bool
	ShowGroup    bool
	FollowLinks  bool
	SortByName   bool
	IgnoreList   []string
	OnlyDirs     bool
	OnlyFiles    bool
	Pattern      string
	HumanSize    bool
	CountOnly    bool
	Color        bool
	Interactive  bool
	SafeMode     bool
	Verbose      bool
	NoLimit      bool
	MaxFileSize  int64
	SkipLarge    bool
	ElispParse   bool
	JsonOutput   bool
	XmlOutput    bool
	Markdown     bool
	Html         bool
	OutputFile   string
	Threads      int
	Progress     bool
	Summary      bool
	ExcludeDirs  []string
	ExcludeFiles []string
	IncludeOnly  []string
	Stats        bool
	Checksum     bool
	GitIgnore    bool
	FollowMount  bool
	BufferSize   int
	Timeout      int
	Retry        int
	DryRun       bool
	Backup       bool
	Force        bool
	Quiet        bool
	Debug        bool
}

// DefaultFileTreeConfig 默认配置
func DefaultFileTreeConfig() *FileTreeConfig {
	return &FileTreeConfig{
		MaxDepth:     20,
		MaxNodes:     100,
		ShowHidden:   false,
		ShowSize:     false,
		ShowTime:     false,
		ShowMode:     false,
		ShowOwner:    false,
		ShowGroup:    false,
		FollowLinks:  false,
		SortByName:   true,
		IgnoreList: []string{
			".git", ".svn", ".hg", ".DS_Store",
			"node_modules", "__pycache__", ".cache",
			"thumbs.db", "desktop.ini", ".Spotlight-V100",
			".Trashes", "._.DS_Store", ".fseventsd",
		},
		OnlyDirs:     false,
		OnlyFiles:    false,
		Pattern:      "",
		HumanSize:    true,
		CountOnly:    false,
		Color:        true,
		Interactive:  false,
		SafeMode:     true,
		Verbose:      false,
		NoLimit:      false,
		MaxFileSize:  100 * 1024 * 1024, // 100MB
		SkipLarge:    true,
		ElispParse:   true,
		JsonOutput:   false,
		XmlOutput:    false,
		Markdown:     false,
		Html:         false,
		OutputFile:   "",
		Threads:      4,
		Progress:     false,
		Summary:      true,
		ExcludeDirs:  []string{},
		ExcludeFiles: []string{},
		IncludeOnly:  []string{},
		Stats:        false,
		Checksum:     false,
		GitIgnore:    true,
		FollowMount:  false,
		BufferSize:   4096,
		Timeout:      30,
		Retry:        3,
		DryRun:       false,
		Backup:       false,
		Force:        false,
		Quiet:        false,
		Debug:        false,
	}
}

// DetailedError 详细错误信息
type DetailedError struct {
	Path      string
	Operation string
	Err       error
	Advice    string
	Severity  string // "warning", "error", "info"
	Code      string
	Timestamp time.Time
	User      string
	PID       int
}

// PermissionAwareFileTree 权限感知的文件树
type PermissionAwareFileTree struct {
	Root            *FileNode
	Config          *FileTreeConfig
	nodeCount       int
	dirCount        int
	fileCount       int
	sizeTotal       int64
	errors          []*DetailedError
	warnings        []*DetailedError
	skipCount       int
	permissionStats map[string]int
	startTime       time.Time
	endTime         time.Time
	user            *user.User
	isRoot          bool
	osType          string
	totalEntries    int
	processedEntries int
	largeFiles      []string
	symlinks        []string
	brokenLinks     []string
	elispFiles      []string
	executables     []string
	archives        []string
	images          []string
	videos          []string
	documents       []string
	codeFiles       []string
	configFiles     []string
	tempFiles       []string
	lockFiles       []string
	logFiles        []string
	backupFiles     []string
	hiddenFiles     []string
	emptyDirs       []string
	emptyFiles      []string
	zeroSizeFiles   []string
	duplicates      map[string][]string
	checksums       map[string]string
	permissions     map[string]string
	owners          map[string]string
	groups          map[string]string
	extensions      map[string]int
	depthStats      map[int]int
	fileAgeStats    map[string]int
	sizeStats       map[string]int
	threadPool      chan struct{}
	progressChan    chan ProgressUpdate
	stopChan        chan struct{}
	resultsChan     chan *FileNode
	errorChan       chan error
	doneChan        chan bool
	cancelFunc      func()
	context         *Context
}

// Context 上下文
type Context struct {
	Cancel   func()
	Done     <-chan struct{}
	Deadline time.Time
	Timeout  time.Duration
	Values   map[interface{}]interface{}
}

// ProgressUpdate 进度更新
type ProgressUpdate struct {
	Processed int
	Total     int
	Current   string
	Speed     float64
	ETA       time.Duration
	Percent   float64
	Remaining int
	Errors    int
	Warnings  int
	Skipped   int
	Time      time.Time
}

// ==================== 辅助函数 ====================

// 颜色定义
const (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Italic  = "\033[3m"
	Underline = "\033[4m"
	Blink   = "\033[5m"
	Reverse = "\033[7m"
	Hidden  = "\033[8m"
)

// 获取颜色
func getColor(code string) string {
	if !globalConfig.Color {
		return ""
	}
	return code
}

// 格式化时间间隔
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

// 格式化文件大小
func formatSize(bytes int64, human bool) string {
	if !human {
		return fmt.Sprintf("%d", bytes)
	}
	
	if bytes < 0 {
		return "0B"
	}
	
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	
	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	return fmt.Sprintf("%.1f%s", float64(bytes)/float64(div), sizes[exp+1])
}

// 检查是否在忽略列表中
func isInIgnoreList(name string, ignoreList []string) bool {
	for _, ignore := range ignoreList {
		if name == ignore {
			return true
		}
		// 支持通配符
		if matched, _ := filepath.Match(ignore, name); matched {
			return true
		}
	}
	return false
}

// 检查是否匹配模式
func matchesPattern(name, pattern string) bool {
	if pattern == "" {
		return true
	}
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// 获取文件类型图标
func getFileTypeIcon(nodeType FileNodeType, hasError bool) string {
	if hasError {
		return "🚫"
	}
	
	switch nodeType {
	case FileTypeDirectory:
		return "📁"
	case FileTypeSymlink:
		return "🔗"
	case FileTypeExecutable:
		return "⚡"
	case FileTypeHidden:
		return "👁️"
	case FileTypeElisp:
		return "λ"
	case FileTypePermissionDenied:
		return "🔒"
	default:
		return "📄"
	}
}

// 获取文件类型颜色
func getFileTypeColor(nodeType FileNodeType, hasError bool) string {
	if hasError {
		return Red
	}
	
	switch nodeType {
	case FileTypeDirectory:
		return Blue
	case FileTypeSymlink:
		return Cyan
	case FileTypeExecutable:
		return Green
	case FileTypeElisp:
		return Magenta
	default:
		return ""
	}
}

// 检查权限
func checkPermission(path string, mode os.FileMode, isDir bool) (bool, string) {
	// 检查读取权限
	if syscall.Access(path, syscall.R_OK) != nil {
		return false, "读取权限被拒绝"
	}
	
	// 如果是目录，检查执行权限
	if isDir && syscall.Access(path, syscall.X_OK) != nil {
		return false, "目录执行权限被拒绝"
	}
	
	return true, ""
}

// 获取文件所有者信息
func getFileOwner(path string) (string, string, error) {
	if runtime.GOOS == "windows" {
		return "SYSTEM", "SYSTEM", nil
	}
	
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", fmt.Errorf("无法获取文件状态")
	}
	
	// 获取用户名
	u, err := user.LookupId(fmt.Sprintf("%d", stat.Uid))
	var username string
	if err != nil {
		username = fmt.Sprintf("%d", stat.Uid)
	} else {
		username = u.Username
	}
	
	// 获取组名
	g, err := user.LookupGroupId(fmt.Sprintf("%d", stat.Gid))
	var groupname string
	if err != nil {
		groupname = fmt.Sprintf("%d", stat.Gid)
	} else {
		groupname = g.Name
	}
	
	return username, groupname, nil
}

// 格式化权限字符串
func formatPermissions(mode os.FileMode) string {
	perm := mode.String()
	if len(perm) > 10 {
		return perm[1:]
	}
	return perm
}

// 解析Elisp文件
func parseElispFile(path string) ([]*FileNode, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	
	var nodes []*FileNode
	lines := strings.Split(string(content), "\n")
	
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		
		node := &FileNode{
			Name:  fmt.Sprintf("Line %d: %s", i+1, truncateString(line, 50)),
			Type:  FileTypeElisp,
			Depth: 1,
		}
		
		// 尝试解析函数定义
		if strings.HasPrefix(line, "(def") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				node.Name = fmt.Sprintf("λ %s", parts[1])
			}
		}
		
		nodes = append(nodes, node)
	}
	
	return nodes, nil
}

// 截断字符串
func truncateString(s string, maxLength int) string {
	if len(s) <= maxLength {
		return s
	}
	return s[:maxLength-3] + "..."
}

// 确认提示
func confirm(prompt string) bool {
	fmt.Print(prompt)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// 打印横幅
func printBanner() {
	banner := `
███████╗██╗██╗     ███████╗████████╗██████╗ ███████╗███████╗
██╔════╝██║██║     ██╔════╝╚══██╔══╝██╔══██╗██╔════╝██╔════╝
█████╗  ██║██║     █████╗     ██║   ██████╔╝█████╗  █████╗  
██╔══╝  ██║██║     ██╔══╝     ██║   ██╔══██╗██╔══╝  ██╔══╝  
██║     ██║███████╗███████╗   ██║   ██║  ██║███████╗███████╗
╚═╝     ╚═╝╚══════╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚══════╝
`
	fmt.Println(getColor(Cyan) + banner + getColor(Reset))
	fmt.Printf("%s版本: %s%s\n", getColor(Yellow), version, getColor(Reset))
	fmt.Printf("%s作者: %s%s\n\n", getColor(Dim), author, getColor(Reset))
}

// ==================== 文件树构建 ====================

// NewPermissionAwareFileTree 创建权限感知的文件树
func NewPermissionAwareFileTree(config *FileTreeConfig) *PermissionAwareFileTree {
	currentUser, _ := user.Current()
	isRoot := currentUser.Uid == "0"
	
	return &PermissionAwareFileTree{
		Config:          config,
		errors:          []*DetailedError{},
		warnings:        []*DetailedError{},
		permissionStats: make(map[string]int),
		startTime:       time.Now(),
		user:            currentUser,
		isRoot:          isRoot,
		osType:          runtime.GOOS,
		largeFiles:      []string{},
		symlinks:        []string{},
		brokenLinks:     []string{},
		elispFiles:      []string{},
		executables:     []string{},
		archives:        []string{},
		images:          []string{},
		videos:          []string{},
		documents:       []string{},
		codeFiles:       []string{},
		configFiles:     []string{},
		tempFiles:       []string{},
		lockFiles:       []string{},
		logFiles:        []string{},
		backupFiles:     []string{},
		hiddenFiles:     []string{},
		emptyDirs:       []string{},
		emptyFiles:      []string{},
		zeroSizeFiles:   []string{},
		duplicates:      make(map[string][]string),
		checksums:       make(map[string]string),
		permissions:     make(map[string]string),
		owners:          make(map[string]string),
		groups:          make(map[string]string),
		extensions:      make(map[string]int),
		depthStats:      make(map[int]int),
		fileAgeStats:    make(map[string]int),
		sizeStats:       make(map[string]int),
		threadPool:      make(chan struct{}, config.Threads),
		progressChan:    make(chan ProgressUpdate, 100),
		stopChan:        make(chan struct{}),
		resultsChan:     make(chan *FileNode, 1000),
		errorChan:       make(chan error, 100),
		doneChan:        make(chan bool),
	}
}

// BuildFromPath 从路径构建文件树
func (ft *PermissionAwareFileTree) BuildFromPath(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ft.addError("获取绝对路径", path, err, "检查路径格式")
	}
	
	// 检查路径是否存在
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return ft.addError("访问路径", absPath, err, "路径不存在")
	}
	
	// 检查权限
	hasPermission, permErr := checkPermission(absPath, 0, false)
	if !hasPermission {
		ft.permissionStats["root_access_denied"]++
		ft.warnings = append(ft.warnings, &DetailedError{
			Path:      absPath,
			Operation: "访问路径",
			Err:       fmt.Errorf("%s", permErr),
			Advice:    "尝试使用管理员权限或检查文件权限",
			Severity:  "warning",
		})
		
		// 尝试获取基本信息
		info, statErr := os.Stat(absPath)
		if statErr != nil {
			return ft.addError("获取文件信息", absPath, statErr, "无权限访问")
		}
		
		// 创建受限的根节点
		ft.Root = &FileNode{
			Name:  filepath.Base(absPath),
			Path:  absPath,
			Type:  FileTypePermissionDenied,
			Error: "权限被拒绝: " + permErr,
		}
		return nil
	}
	
	info, err := os.Stat(absPath)
	if err != nil {
		return ft.addError("获取文件信息", absPath, err, "检查文件系统")
	}
	
	// 获取所有者信息
	owner, group, _ := getFileOwner(absPath)
	
	// 创建根节点
	ft.Root = &FileNode{
		Name:     filepath.Base(absPath),
		Path:     absPath,
		Type:     FileTypeDirectory,
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		Mode:     info.Mode(),
		Children: []*FileNode{},
		Owner:    owner,
		Group:    group,
		Perm:     formatPermissions(info.Mode()),
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
		return ft.handleFile(ft.Root)
	}
}

// buildDirectoryTree 构建目录树
func (ft *PermissionAwareFileTree) buildDirectoryTree(node *FileNode, depth int) error {
	if depth > ft.Config.MaxDepth && !ft.Config.NoLimit {
		ft.warnings = append(ft.warnings, &DetailedError{
			Path:      node.Path,
			Operation: "遍历目录",
			Err:       fmt.Errorf("达到最大深度 %d", ft.Config.MaxDepth),
			Advice:    "使用 --max-depth 增加深度限制",
			Severity:  "info",
		})
		return nil
	}
	
	// 检查节点限制
	if ft.nodeCount >= ft.Config.MaxNodes && !ft.Config.NoLimit {
		ft.warnings = append(ft.warnings, &DetailedError{
			Path:      node.Path,
			Operation: "遍历目录",
			Err:       fmt.Errorf("达到最大节点数 %d", ft.Config.MaxNodes),
			Advice:    "使用 --max-nodes 增加节点限制或使用 --no-limit",
			Severity:  "info",
		})
		return nil
	}
	
	// 读取目录
	entries, err := ioutil.ReadDir(node.Path)
	if err != nil {
		ft.permissionStats["read_denied"]++
		ft.warnings = append(ft.warnings, &DetailedError{
			Path:      node.Path,
			Operation: "读取目录",
			Err:       err,
			Advice:    "检查目录权限或尝试以管理员权限运行",
			Severity:  "warning",
		})
		
		node.Children = append(node.Children, &FileNode{
			Name:  fmt.Sprintf("🚫 无法读取目录: %v", err),
			Type:  FileTypePermissionDenied,
			Depth: depth,
		})
		return nil
	}
	
	// 过滤和排序条目
	entries = ft.filterEntries(entries, node.Path)
	
	// 更新统计
	ft.totalEntries += len(entries)
	
	// 处理每个条目
	for i, entry := range entries {
		if ft.Config.Progress && i%10 == 0 {
			ft.progressChan <- ProgressUpdate{
				Processed: ft.processedEntries + i,
				Total:     ft.totalEntries,
				Current:   entry.Name(),
				Percent:   float64(ft.processedEntries+i) / float64(ft.totalEntries) * 100,
			}
		}
		
		ft.processedEntries++
		
		if err := ft.processEntry(node, entry, depth, i == len(entries)-1); err != nil {
			ft.errorChan <- err
		}
		
		// 检查停止信号
		select {
		case <-ft.stopChan:
			return fmt.Errorf("遍历被中断")
		default:
		}
	}
	
	return nil
}

// filterEntries 过滤条目
func (ft *PermissionAwareFileTree) filterEntries(entries []os.FileInfo, parentPath string) []os.FileInfo {
	var filtered []os.FileInfo
	
	for _, entry := range entries {
		name := entry.Name()
		
		// 跳过隐藏文件
		if !ft.Config.ShowHidden && strings.HasPrefix(name, ".") {
			ft.hiddenFiles = append(ft.hiddenFiles, filepath.Join(parentPath, name))
			continue
		}
		
		// 检查忽略列表
		if isInIgnoreList(name, ft.Config.IgnoreList) {
			continue
		}
		
		// 检查排除目录
		if entry.IsDir() && isInIgnoreList(name, ft.Config.ExcludeDirs) {
			continue
		}
		
		// 检查排除文件
		if !entry.IsDir() && isInIgnoreList(name, ft.Config.ExcludeFiles) {
			continue
		}
		
		// 检查包含列表
		if len(ft.Config.IncludeOnly) > 0 && !isInIgnoreList(name, ft.Config.IncludeOnly) {
			continue
		}
		
		// 检查模式匹配
		if !matchesPattern(name, ft.Config.Pattern) {
			continue
		}
		
		// 检查文件类型过滤
		if ft.Config.OnlyDirs && !entry.IsDir() {
			continue
		}
		if ft.Config.OnlyFiles && entry.IsDir() {
			continue
		}
		
		filtered = append(filtered, entry)
	}
	
	// 排序
	if ft.Config.SortByName {
		// 这里可以添加更复杂的排序逻辑
	}
	
	return filtered
}

// processEntry 处理条目
func (ft *PermissionAwareFileTree) processEntry(parent *FileNode, entry os.FileInfo, depth int, isLast bool) error {
	entryPath := filepath.Join(parent.Path, entry.Name())
	
	// 检查权限
	hasPermission, permErr := checkPermission(entryPath, entry.Mode(), entry.IsDir())
	if !hasPermission {
		ft.permissionStats["access_denied"]++
		
		owner, group, _ := getFileOwner(entryPath)
		perm := formatPermissions(entry.Mode())
		
		deniedNode := &FileNode{
			Name:     fmt.Sprintf("🔒 %s", entry.Name()),
			Path:     entryPath,
			Type:     FileTypePermissionDenied,
			Size:     entry.Size(),
			ModTime:  entry.ModTime(),
			Mode:     entry.Mode(),
			Children: []*FileNode{},
			Depth:    depth,
			IsLast:   isLast,
			Error:    fmt.Sprintf("权限被拒绝: %s", permErr),
			Owner:    owner,
			Group:    group,
			Perm:     perm,
		}
		
		parent.Children = append(parent.Children, deniedNode)
		ft.nodeCount++
		ft.skipCount++
		return nil
	}
	
	// 获取所有者信息
	owner, group, _ := getFileOwner(entryPath)
	perm := formatPermissions(entry.Mode())
	
	// 确定文件类型
	var nodeType FileNodeType
	var node *FileNode
	
	switch {
	case entry.IsDir():
		nodeType = FileTypeDirectory
		ft.dirCount++
		
		// 检查是否为空目录
		subEntries, _ := ioutil.ReadDir(entryPath)
		if len(subEntries) == 0 {
			ft.emptyDirs = append(ft.emptyDirs, entryPath)
		}
		
	case entry.Mode()&os.ModeSymlink != 0:
		nodeType = FileTypeSymlink
		ft.fileCount++
		ft.symlinks = append(ft.symlinks, entryPath)
		
		// 检查是否为损坏的符号链接
		if _, err := os.Stat(entryPath); os.IsNotExist(err) {
			ft.brokenLinks = append(ft.brokenLinks, entryPath)
		}
		
	case entry.Mode()&0111 != 0:
		nodeType = FileTypeExecutable
		ft.fileCount++
		ft.executables = append(ft.executables, entryPath)
		
	case strings.HasPrefix(entry.Name(), "."):
		nodeType = FileTypeHidden
		ft.fileCount++
		ft.hiddenFiles = append(ft.hiddenFiles, entryPath)
		
	default:
		// 根据扩展名分类
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		ft.extensions[ext]++
		
		switch ext {
		case ".el", ".elc", ".el.gz":
			nodeType = FileTypeElisp
			ft.elispFiles = append(ft.elispFiles, entryPath)
		case ".go", ".py", ".js", ".ts", ".java", ".cpp", ".c", ".h", ".rs":
			ft.codeFiles = append(ft.codeFiles, entryPath)
			nodeType = FileTypeRegular
		case ".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf":
			ft.configFiles = append(ft.configFiles, entryPath)
			nodeType = FileTypeRegular
		case ".log", ".txt", ".out":
			ft.logFiles = append(ft.logFiles, entryPath)
			nodeType = FileTypeRegular
		case ".tmp", ".temp", ".swp", ".swo":
			ft.tempFiles = append(ft.tempFiles, entryPath)
			nodeType = FileTypeRegular
		case ".lock":
			ft.lockFiles = append(ft.lockFiles, entryPath)
			nodeType = FileTypeRegular
		case ".bak", ".backup", ".old":
			ft.backupFiles = append(ft.backupFiles, entryPath)
			nodeType = FileTypeRegular
		case ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar":
			ft.archives = append(ft.archives, entryPath)
			nodeType = FileTypeRegular
		case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg", ".webp":
			ft.images = append(ft.images, entryPath)
			nodeType = FileTypeRegular
		case ".mp4", ".avi", ".mkv", ".mov", ".wmv", ".flv":
			ft.videos = append(ft.videos, entryPath)
			nodeType = FileTypeRegular
		case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx":
			ft.documents = append(ft.documents, entryPath)
			nodeType = FileTypeRegular
		default:
			nodeType = FileTypeRegular
		}
		ft.fileCount++
	}
	
	// 检查文件大小
	if entry.Size() == 0 {
		ft.zeroSizeFiles = append(ft.zeroSizeFiles, entryPath)
		if !entry.IsDir() {
			ft.emptyFiles = append(ft.emptyFiles, entryPath)
		}
	} else if entry.Size() > ft.Config.MaxFileSize && ft.Config.SkipLarge {
		ft.largeFiles = append(ft.largeFiles, entryPath)
		ft.skipCount++
		return nil
	}
	
	// 创建节点
	node = &FileNode{
		Name:     entry.Name(),
		Path:     entryPath,
		Type:     nodeType,
		Size:     entry.Size(),
		ModTime:  entry.ModTime(),
		Mode:     entry.Mode(),
		Children: []*FileNode{},
		Depth:    depth,
		IsLast:   isLast,
		Owner:    owner,
		Group:    group,
		Perm:     perm,
	}
	
	// 更新统计
	ft.sizeTotal += entry.Size()
	ft.nodeCount++
	ft.depthStats[depth]++
	
	// 添加到父节点
	parent.Children = append(parent.Children, node)
	
	// 如果是目录，递归处理
	if entry.IsDir() && ft.Config.FollowLinks {
		return ft.buildDirectoryTree(node, depth+1)
	}
	
	return nil
}

// handleFile 处理文件
func (ft *PermissionAwareFileTree) handleFile(node *FileNode) error {
	// 检查是否为Elisp文件
	if ft.Config.ElispParse && strings.HasSuffix(strings.ToLower(node.Path), ".el") {
		children, err := parseElispFile(node.Path)
		if err == nil {
			node.Children = children
			node.Type = FileTypeElisp
		}
	}
	return nil
}

// addError 添加错误
func (ft *PermissionAwareFileTree) addError(operation, path string, err error, advice string) error {
	detailedErr := &DetailedError{
		Path:      path,
		Operation: operation,
		Err:       err,
		Advice:    advice,
		Severity:  "error",
		Timestamp: time.Now(),
		User:      ft.user.Username,
		PID:       os.Getpid(),
	}
	ft.errors = append(ft.errors, detailedErr)
	return detailedErr
}

// ==================== 打印和输出 ====================

// PrintTree 打印树
func (ft *PermissionAwareFileTree) PrintTree() {
	if ft.Config.CountOnly {
		ft.printCounts()
		return
	}
	
	if ft.Root == nil {
		fmt.Println("🌳 树为空")
		return
	}
	
	// 打印摘要
	if ft.Config.Summary {
		ft.printSummary()
	}
	
	// 打印警告和错误
	if !ft.Config.Quiet {
		ft.printWarningsAndErrors()
	}
	
	// 打印树结构
	fmt.Println()
	ft.printNode(ft.Root, "", true)
	
	// 打印提示
	if !ft.Config.Quiet {
		ft.printTips()
	}
	
	// 打印统计信息
	if ft.Config.Stats {
		ft.printStatistics()
	}
	
	// 保存到文件
	if ft.Config.OutputFile != "" {
		ft.saveToFile()
	}
}

// printNode 打印节点
func (ft *PermissionAwareFileTree) printNode(node *FileNode, prefix string, isLast bool) {
	// 构建前缀
	linePrefix := prefix
	if prefix != "" {
		if isLast {
			linePrefix += "└── "
		} else {
			linePrefix += "├── "
		}
	}
	
	// 格式化节点文本
	nodeText := ft.formatNode(node)
	
	// 打印节点
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

// formatNode 格式化节点
func (ft *PermissionAwareFileTree) formatNode(node *FileNode) string {
	var parts []string
	
	// 添加图标
	icon := getFileTypeIcon(node.Type, node.Error != "")
	if ft.Config.Color {
		color := getFileTypeColor(node.Type, node.Error != "")
		if color != "" {
			icon = color + icon + Reset
		}
	}
	parts = append(parts, icon)
	
	// 添加名称
	name := node.Name
	if ft.Config.Color {
		color := getFileTypeColor(node.Type, node.Error != "")
		if color != "" {
			name = color + name + Reset
		} else if node.Error != "" {
			name = Yellow + name + Reset
		}
	}
	parts = append(parts, name)
	
	// 添加错误信息
	if node.Error != "" && ft.Config.Verbose {
		parts = append(parts, fmt.Sprintf("[%s]", node.Error))
	}
	
	// 添加权限信息
	if ft.Config.ShowMode && node.Perm != "" {
		parts = append(parts, node.Perm)
	}
	
	// 添加所有者信息
	if ft.Config.ShowOwner && node.Owner != "" {
		parts = append(parts, "@"+node.Owner)
	}
	
	// 添加组信息
	if ft.Config.ShowGroup && node.Group != "" {
		parts = append(parts, ":"+node.Group)
	}
	
	// 添加大小
	if ft.Config.ShowSize && node.Size > 0 {
		sizeStr := formatSize(node.Size, ft.Config.HumanSize)
		parts = append(parts, "("+sizeStr+")")
	}
	
	// 添加时间
	if ft.Config.ShowTime && !node.ModTime.IsZero() {
		timeStr := node.ModTime.Format("2006-01-02 15:04")
		parts = append(parts, timeStr)
	}
	
	return strings.Join(parts, " ")
}

// printSummary 打印摘要
func (ft *PermissionAwareFileTree) printSummary() {
	ft.endTime = time.Now()
	duration := ft.endTime.Sub(ft.startTime)
	
	fmt.Printf("%s📁 路径:%s %s\n", getColor(Bold), Reset, ft.Root.Path)
	fmt.Printf("%s📊 统计:%s %d 目录, %d 文件, %d 节点", 
		getColor(Bold), Reset, ft.dirCount, ft.fileCount, ft.nodeCount)
	
	if ft.skipCount > 0 {
		fmt.Printf(", %s%d 个被跳过%s", getColor(Yellow), ft.skipCount, Reset)
	}
	fmt.Println()
	
	if ft.sizeTotal > 0 {
		fmt.Printf("%s💾 总大小:%s %s\n", getColor(Bold), Reset, formatSize(ft.sizeTotal, true))
	}
	
	fmt.Printf("%s⏱️  耗时:%s %s\n", getColor(Bold), Reset, formatDuration(duration))
	
	if ft.user != nil {
		fmt.Printf("%s👤 用户:%s %s (UID: %s)\n", getColor(Bold), Reset, ft.user.Username, ft.user.Uid)
	}
	
	if ft.isRoot {
		fmt.Printf("%s⚠️  警告:%s 您正在以 root 用户运行\n", getColor(Red+Bold), Reset)
	}
}

// printWarningsAndErrors 打印警告和错误
func (ft *PermissionAwareFileTree) printWarningsAndErrors() {
	// 打印错误
	if len(ft.errors) > 0 {
		fmt.Printf("\n%s❌ 错误 (%d):%s\n", getColor(Red+Bold), len(ft.errors), Reset)
		for _, err := range ft.errors {
			fmt.Printf("  • %s: %v\n", err.Operation, err.Err)
			if err.Advice != "" && ft.Config.Verbose {
				fmt.Printf("    建议: %s\n", err.Advice)
			}
		}
	}
	
	// 打印警告
	if len(ft.warnings) > 0 {
		fmt.Printf("\n%s⚠️  警告 (%d):%s\n", getColor(Yellow+Bold), len(ft.warnings), Reset)
		for _, warning := range ft.warnings {
			fmt.Printf("  • %s: %v\n", warning.Operation, warning.Err)
			if warning.Advice != "" && ft.Config.Verbose {
				fmt.Printf("    建议: %s\n", warning.Advice)
			}
		}
	}
	
	// 打印权限统计
	if ft.permissionStats["access_denied"] > 0 || ft.permissionStats["read_denied"] > 0 {
		fmt.Printf("\n%s🔐 权限统计:%s\n", getColor(Bold), Reset)
		for permType, count := range ft.permissionStats {
			if count > 0 {
				fmt.Printf("  • %s: %d\n", permType, count)
			}
		}
	}
}

// printTips 打印提示
func (ft *PermissionAwareFileTree) printTips() {
	if ft.nodeCount >= ft.Config.MaxNodes && !ft.Config.NoLimit {
		fmt.Printf("\n%s⚠️  节点数已达限制 (%d)，已停止遍历%s\n", 
			getColor(Yellow), ft.Config.MaxNodes, Reset)
		fmt.Printf("   使用 %s--max-nodes%s 参数调整限制\n", 
			getColor(Cyan), Reset)
		fmt.Printf("   或使用 %s--no-limit%s 取消限制\n", 
			getColor(Cyan), Reset)
	}
	
	if ft.skipCount > 0 {
		fmt.Printf("\n%s💡 权限提示:%s\n", getColor(Bold), Reset)
		fmt.Println("   如果您需要访问被跳过的文件/目录:")
		fmt.Println("   1. 使用管理员权限: sudo " + os.Args[0] + " [路径]")
		fmt.Println("   2. 修改文件权限: chmod -R 755 [路径]")
		fmt.Println("   3. 修改文件所有者: chown -R $USER:$USER [路径]")
		fmt.Println("   4. 检查SELinux状态: getenforce 和 ls -Z [路径]")
	}
	
	if len(ft.brokenLinks) > 0 && ft.Config.Verbose {
		fmt.Printf("\n%s🔗 损坏的符号链接:%s\n", getColor(Yellow), Reset)
		for _, link := range ft.brokenLinks {
			fmt.Printf("  • %s\n", link)
		}
	}
}

// printStatistics 打印统计信息
func (ft *PermissionAwareFileTree) printStatistics() {
	fmt.Printf("\n%s📈 详细统计:%s\n", getColor(Bold), Reset)
	
	// 文件类型统计
	fmt.Println("  📁 文件类型分布:")
	fmt.Printf("    • 目录: %d\n", ft.dirCount)
	fmt.Printf("    • 文件: %d\n", ft.fileCount)
	fmt.Printf("    • 符号链接: %d\n", len(ft.symlinks))
	fmt.Printf("    • 可执行文件: %d\n", len(ft.executables))
	fmt.Printf("    • Elisp文件: %d\n", len(ft.elispFiles))
	
	// 特殊文件统计
	if len(ft.emptyDirs) > 0 {
		fmt.Printf("    • 空目录: %d\n", len(ft.emptyDirs))
	}
	if len(ft.emptyFiles) > 0 {
		fmt.Printf("    • 空文件: %d\n", len(ft.emptyFiles))
	}
	if len(ft.largeFiles) > 0 {
		fmt.Printf("    • 大文件(>%s): %d\n", 
			formatSize(ft.Config.MaxFileSize, true), len(ft.largeFiles))
	}
	
	// 扩展名统计
	if len(ft.extensions) > 0 {
		fmt.Println("\n  📄 扩展名统计:")
		for ext, count := range ft.extensions {
			if count > 5 { // 只显示常见的扩展名
				fmt.Printf("    • %s: %d\n", ext, count)
			}
		}
	}
	
	// 深度统计
	if len(ft.depthStats) > 0 {
		fmt.Println("\n  📊 深度分布:")
		for depth, count := range ft.depthStats {
			fmt.Printf("    • 深度 %d: %d 个节点\n", depth, count)
		}
	}
}

// printCounts 仅打印计数
func (ft *PermissionAwareFileTree) printCounts() {
	fmt.Printf("目录: %d\n", ft.dirCount)
	fmt.Printf("文件: %d\n", ft.fileCount)
	fmt.Printf("总计: %d\n", ft.nodeCount)
	fmt.Printf("跳过: %d\n", ft.skipCount)
	fmt.Printf("大小: %s\n", formatSize(ft.sizeTotal, true))
	
	if ft.Config.Verbose {
		for permType, count := range ft.permissionStats {
			if count > 0 {
				fmt.Printf("%s: %d\n", permType, count)
			}
		}
	}
}

// saveToFile 保存到文件
func (ft *PermissionAwareFileTree) saveToFile() error {
	var content strings.Builder
	
	// 构建输出内容
	content.WriteString("# 文件树导出\n\n")
	content.WriteString(fmt.Sprintf("路径: %s\n", ft.Root.Path))
	content.WriteString(fmt.Sprintf("时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	content.WriteString(fmt.Sprintf("用户: %s\n\n", ft.user.Username))
	
	// 添加树结构
	ft.writeNodeToBuffer(&content, ft.Root, "", true)
	
	// 保存到文件
	err := ioutil.WriteFile(ft.Config.OutputFile, []byte(content.String()), 0644)
	if err != nil {
		return fmt.Errorf("保存文件失败: %v", err)
	}
	
	fmt.Printf("\n%s✅ 树结构已保存到: %s%s\n", getColor(Green), ft.Config.OutputFile, Reset)
	return nil
}

// writeNodeToBuffer 写入节点到缓冲区
func (ft *PermissionAwareFileTree) writeNodeToBuffer(builder *strings.Builder, node *FileNode, prefix string, isLast bool) {
	linePrefix := prefix
	if prefix != "" {
		if isLast {
			linePrefix += "└── "
		} else {
			linePrefix += "├── "
		}
	}
	
	nodeText := ft.formatNode(node)
	builder.WriteString(linePrefix + nodeText + "\n")
	
	childPrefix := prefix
	if prefix != "" {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}
	
	for i, child := range node.Children {
		isLastChild := i == len(node.Children)-1
		ft.writeNodeToBuffer(builder, child, childPrefix, isLastChild)
	}
}

// ==================== 命令行界面 ====================

var (
	globalConfig *FileTreeConfig
)

func main() {
	// 解析命令行参数
	parseFlags()
	
	// 打印横幅
	if !globalConfig.Quiet {
		printBanner()
	}
	
	// 获取要扫描的路径
	args := flag.Args()
	var path string
	if len(args) > 0 {
		path = args[0]
	} else {
		path = "."
	}
	
	// 检查路径
	if path == "" || path == "." {
		var err error
		path, err = os.Getwd()
		if err != nil {
			fmt.Printf("%s❌ 无法获取当前目录: %v%s\n", Red, err, Reset)
			os.Exit(1)
		}
	}
	
	// 检查路径是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("%s❌ 路径不存在: %s%s\n", Red, path, Reset)
		fmt.Printf("请检查路径是否正确，或使用绝对路径\n")
		os.Exit(1)
	}
	
	// 创建文件树
	tree := NewPermissionAwareFileTree(globalConfig)
	
	// 构建树
	if globalConfig.Progress && !globalConfig.Quiet {
		fmt.Printf("%s🔍 正在扫描 %s...%s\n", Cyan, path, Reset)
	}
	
	err := tree.BuildFromPath(path)
	if err != nil {
		fmt.Printf("%s❌ 构建文件树失败: %v%s\n", Red, err, Reset)
		os.Exit(1)
	}
	
	// 打印树
	tree.PrintTree()
	
	// 退出码
	if len(tree.errors) > 0 {
		os.Exit(1)
	}
}

// parseFlags 解析命令行标志
func parseFlags() {
	globalConfig = DefaultFileTreeConfig()
	
	flag.StringVar(&globalConfig.Pattern, "pattern", "", "文件模式匹配 (如 *.go)")
	flag.StringVar(&globalConfig.OutputFile, "output", "", "输出到文件")
	
	flag.IntVar(&globalConfig.MaxDepth, "max-depth", 20, "最大遍历深度")
	flag.IntVar(&globalConfig.MaxNodes, "max-nodes", 100, "最大节点数")
	flag.IntVar(&globalConfig.Threads, "threads", 4, "并发线程数")
	flag.IntVar(&globalConfig.Timeout, "timeout", 30, "超时时间(秒)")
	flag.IntVar(&globalConfig.Retry, "retry", 3, "重试次数")
	flag.IntVar(&globalConfig.BufferSize, "buffer", 4096, "缓冲区大小")
	
	var maxFileSize string
	flag.StringVar(&maxFileSize, "max-size", "100MB", "最大文件大小")
	
	var ignoreList string
	flag.StringVar(&ignoreList, "ignore", "", "忽略列表，逗号分隔")
	
	var excludeDirs string
	flag.StringVar(&excludeDirs, "exclude-dirs", "", "排除目录，逗号分隔")
	
	var excludeFiles string
	flag.StringVar(&excludeFiles, "exclude-files", "", "排除文件，逗号分隔")
	
	var includeOnly string
	flag.StringVar(&includeOnly, "include-only", "", "仅包含，逗号分隔")
	
	flag.BoolVar(&globalConfig.ShowHidden, "all", false, "显示隐藏文件")
	flag.BoolVar(&globalConfig.ShowSize, "size", false, "显示文件大小")
	flag.BoolVar(&globalConfig.ShowTime, "time", false, "显示修改时间")
	flag.BoolVar(&globalConfig.ShowMode, "mode", false, "显示文件权限")
	flag.BoolVar(&globalConfig.ShowOwner, "owner", false, "显示文件所有者")
	flag.BoolVar(&globalConfig.ShowGroup, "group", false, "显示文件组")
	flag.BoolVar(&globalConfig.FollowLinks, "follow", false, "跟随符号链接")
	flag.BoolVar(&globalConfig.OnlyDirs, "dirs", false, "只显示目录")
	flag.BoolVar(&globalConfig.OnlyFiles, "files", false, "只显示文件")
	flag.BoolVar(&globalConfig.HumanSize, "human", true, "人类可读的文件大小")
	flag.BoolVar(&globalConfig.CountOnly, "count", false, "仅显示计数")
	flag.BoolVar(&globalConfig.Color, "color", true, "彩色输出")
	flag.BoolVar(&globalConfig.Interactive, "interactive", false, "交互模式")
	flag.BoolVar(&globalConfig.SafeMode, "safe", true, "安全模式")
	flag.BoolVar(&globalConfig.Verbose, "verbose", false, "详细模式")
	flag.BoolVar(&globalConfig.NoLimit, "no-limit", false, "无限制模式")
	flag.BoolVar(&globalConfig.SkipLarge, "skip-large", true, "跳过大文件")
	flag.BoolVar(&globalConfig.ElispParse, "elisp", true, "解析Elisp文件")
	flag.BoolVar(&globalConfig.JsonOutput, "json", false, "JSON输出")
	flag.BoolVar(&globalConfig.XmlOutput, "xml", false, "XML输出")
	flag.BoolVar(&globalConfig.Markdown, "markdown", false, "Markdown输出")
	flag.BoolVar(&globalConfig.Html, "html", false, "HTML输出")
	flag.BoolVar(&globalConfig.Progress, "progress", false, "显示进度")
	flag.BoolVar(&globalConfig.Summary, "summary", true, "显示摘要")
	flag.BoolVar(&globalConfig.Stats, "stats", false, "显示统计信息")
	flag.BoolVar(&globalConfig.Checksum, "checksum", false, "计算校验和")
	flag.BoolVar(&globalConfig.GitIgnore, "gitignore", true, "遵守.gitignore")
	flag.BoolVar(&globalConfig.FollowMount, "follow-mount", false, "跟随挂载点")
	flag.BoolVar(&globalConfig.DryRun, "dry-run", false, "试运行")
	flag.BoolVar(&globalConfig.Backup, "backup", false, "备份文件")
	flag.BoolVar(&globalConfig.Force, "force", false, "强制操作")
	flag.BoolVar(&globalConfig.Quiet, "quiet", false, "安静模式")
	flag.BoolVar(&globalConfig.Debug, "debug", false, "调试模式")
	
	var help bool
	var version bool
	flag.BoolVar(&help, "help", false, "显示帮助")
	flag.BoolVar(&version, "version", false, "显示版本")
	
	flag.Usage = func() {
		fmt.Printf("%s文件树浏览器 v%s%s\n\n", Bold, version, Reset)
		fmt.Printf("用法: %s [选项] [路径]\n\n", filepath.Base(os.Args[0]))
		fmt.Println("选项:")
		flag.PrintDefaults()
		fmt.Println("\n示例:")
		fmt.Println("  ftree .                          # 显示当前目录")
		fmt.Println("  ftree /path/to/dir               # 显示指定目录")
		fmt.Println("  ftree -a -s                      # 显示隐藏文件和大小")
		fmt.Println("  ftree --max-depth 3              # 限制深度为3")
		fmt.Println("  ftree --pattern \"*.go\"           # 只显示Go文件")
		fmt.Println("  ftree --output tree.txt          # 保存到文件")
		fmt.Println("  ftree --verbose --stats          # 详细模式+统计")
		fmt.Println("\n提示:")
		fmt.Println("  • 使用 --no-limit 取消节点数限制")
		fmt.Println("  • 使用 --quiet 减少输出")
		fmt.Println("  • 使用 --dry-run 测试运行")
		fmt.Println("  • 权限问题会以黄色/红色显示")
	}
	
	flag.Parse()
	
	// 处理帮助和版本
	if help {
		flag.Usage()
		os.Exit(0)
	}
	
	if version {
		fmt.Printf("文件树浏览器 v%s\n", version)
		os.Exit(0)
	}
	
	// 处理大小字符串
	if maxFileSize != "" {
		multiplier := int64(1)
		maxFileSize = strings.ToUpper(maxFileSize)
		
		if strings.HasSuffix(maxFileSize, "KB") {
			multiplier = 1024
			maxFileSize = strings.TrimSuffix(maxFileSize, "KB")
		} else if strings.HasSuffix(maxFileSize, "MB") {
			multiplier = 1024 * 1024
			maxFileSize = strings.TrimSuffix(maxFileSize, "MB")
		} else if strings.HasSuffix(maxFileSize, "GB") {
			multiplier = 1024 * 1024 * 1024
			maxFileSize = strings.TrimSuffix(maxFileSize, "GB")
		} else if strings.HasSuffix(maxFileSize, "TB") {
			multiplier = 1024 * 1024 * 1024 * 1024
			maxFileSize = strings.TrimSuffix(maxFileSize, "TB")
		} else if strings.HasSuffix(maxFileSize, "B") {
			maxFileSize = strings.TrimSuffix(maxFileSize, "B")
		}
		
		size, err := strconv.ParseInt(strings.TrimSpace(maxFileSize), 10, 64)
		if err == nil {
			globalConfig.MaxFileSize = size * multiplier
		}
	}
	
	// 处理忽略列表
	if ignoreList != "" {
		globalConfig.IgnoreList = append(globalConfig.IgnoreList, 
			strings.Split(ignoreList, ",")...)
	}
	
	if excludeDirs != "" {
		globalConfig.ExcludeDirs = strings.Split(excludeDirs, ",")
	}
	
	if excludeFiles != "" {
		globalConfig.ExcludeFiles = strings.Split(excludeFiles, ",")
	}
	
	if includeOnly != "" {
		globalConfig.IncludeOnly = strings.Split(includeOnly, ",")
	}
	
	// 交互模式确认
	if globalConfig.Interactive && !globalConfig.Quiet {
		fmt.Printf("您将要扫描: %s\n", args[0])
		if !confirm("是否继续? (y/N): ") {
			fmt.Println("操作已取消")
			os.Exit(0)
		}
	}
}
