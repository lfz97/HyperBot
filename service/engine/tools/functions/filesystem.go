package functionTools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/otiai10/copy"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	pwdToolName   string = "PWD"
	cdToolName    string = "CD"
	lsToolName    string = "LS"
	mkdirToolName string = "Mkdir"
	cpToolName    string = "CP"
	mvToolName    string = "MV"
	globToolName  string = "Glob"
)

// 获取当前工作目录
func PWD(ctx context.Context, req struct {
}) (map[string]string, error) {
	path, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"PWD": path,
	}, nil
}

// 列出指定目录下的文件和子目录
type fileInfo struct {
	Name     string      `json:"Name"`
	Size     int64       `json:"Size"`
	IsDir    bool        `json:"IsDir"`
	Mode     os.FileMode `json:"Mode"`
	ModeTime string      `json:"ModTime"`
}

func LS(ctx context.Context, req struct {
	Path string `json:"Path" jsonschema:"description=Path of the directory to list. Defaults to the current directory."`
}) (map[string]string, error) {
	if req.Path == "" {
		req.Path = "."
	}
	files, err := os.ReadDir(req.Path)
	if err != nil {
		return nil, err
	}
	fileInfos := make([]fileInfo, 0, len(files))
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			return nil, err
		}
		f := fileInfo{
			Name:     info.Name(),
			Size:     info.Size(),
			IsDir:    info.IsDir(),
			Mode:     info.Mode(),
			ModeTime: info.ModTime().String(),
		}
		fileInfos = append(fileInfos, f)
	}
	jsonBytes, err := json.Marshal(fileInfos)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Files": string(jsonBytes),
	}, nil
}

// 切换当前工作目录
func CD(ctx context.Context, req struct {
	Path string `json:"Path" jsonschema:"description=Path of the directory to change into."`
}) (map[string]string, error) {
	if req.Path == "" {
		req.Path = "."
	}
	err := os.Chdir(req.Path)
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"CwdNow": cwd,
	}, nil
}

// 创建目录
func Mkdir(ctx context.Context, req struct {
	Path    string `json:"Path" jsonschema:"description=Path of the directory to create."`
	Parents bool   `json:"Parents" jsonschema:"description=Create parent directories automatically. Defaults to false."`
}) (map[string]string, error) {
	if req.Path == "" {
		return nil, errors.New("`path` can't be empty!")
	}
	if req.Parents {
		err := os.MkdirAll(req.Path, 0755)
		if err != nil {
			return nil, err
		}
	} else {
		err := os.Mkdir(req.Path, 0755)
		if err != nil {
			return nil, err
		}
	}
	return map[string]string{
		"Created": req.Path,
	}, nil
}

// 复制文件或目录
func Copy(ctx context.Context, req struct {
	Src string `json:"Src" jsonschema:"description=Path of the source file or directory."`
	Dst string `json:"Dst" jsonschema:"description=Path of the destination file or directory."`
}) (map[string]string, error) {
	if req.Src == "" || req.Dst == "" {
		return nil, errors.New("`src` and `dst` can't be empty!")
	}
	if req.Src == req.Dst {
		return nil, errors.New("`src` and `dst` can't be the same!")
	}
	if _, err := os.Stat(req.Src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("source path does not exist: %w", err)
		}
		return nil, fmt.Errorf("failed to stat source: %w", err)
	}
	if err := copy.Copy(req.Src, req.Dst); err != nil {
		return nil, fmt.Errorf("failed to copy: %w", err)
	}
	return map[string]string{
		"Copied": fmt.Sprintf("%s → %s", req.Src, req.Dst),
	}, nil
}

// 移动或重命名文件或目录
func MV(ctx context.Context, req struct {
	OldPath string `json:"OldPath" jsonschema:"description=Original path of the file or directory to move or rename."`
	NewPath string `json:"NewPath" jsonschema:"description=New path of the file or directory."`
}) (map[string]string, error) {
	if req.OldPath == "" || req.NewPath == "" {
		return nil, errors.New("`oldPath` and `newPath` can't be empty!")
	}
	if req.OldPath == req.NewPath {
		return nil, errors.New("`oldPath` and `newPath` can't be the same!")
	}
	// 同设备直接重命名
	if err := os.Rename(req.OldPath, req.NewPath); err == nil {
		return map[string]string{
			"OldPath": req.OldPath,
			"NewPath": req.NewPath,
		}, nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return nil, err
	}
	// 跨设备移动：先删目标（避免合并），复制源，再删源
	if err := os.RemoveAll(req.NewPath); err != nil {
		return nil, fmt.Errorf("failed to remove existing destination: %w", err)
	}
	if err := copy.Copy(req.OldPath, req.NewPath); err != nil {
		return nil, fmt.Errorf("failed to copy across devices: %w", err)
	}
	if err := os.RemoveAll(req.OldPath); err != nil {
		return nil, fmt.Errorf("source was moved but could not be removed: %w", err)
	}
	return map[string]string{
		"Moved": fmt.Sprintf("%s → %s", req.OldPath, req.NewPath),
	}, nil
}

func Glob(ctx context.Context, req struct {
	Regex string `json:"Regex" jsonschema:"description=Regular expression matched against file names."`
	Root  string `json:"Root" jsonschema:"description=Path to start searching from. Defaults to the current directory."`
	Depth int    `json:"Depth" jsonschema:"description=Search depth. Defaults to 0 which only covers the same directory; pass -1 for unlimited depth."`
}) (map[string]string, error) {
	if req.Depth < -1 {
		return nil, errors.New("`depth` must be -1 (for unlimited) or a non-negative integer")
	}
	if req.Root == "" {
		req.Root = "."
	}
	_, err := os.Stat(req.Root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("path does not exist: %w", err)
		}
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}
	regex_p, err := regexp.Compile(req.Regex)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	mathesFiles := []string{}
	err = filepath.WalkDir(req.Root, func(path string, d os.DirEntry, err error) error {
		rel, err := filepath.Rel(req.Root, path)
		if err != nil {
			return err
		}
		if rel != "." {
			// 深度 = 相对路径中用分隔符分隔的组件数
			depth := len(strings.Split(rel, string(os.PathSeparator))) - 1
			if depth > req.Depth && req.Depth >= 0 {
				return filepath.SkipDir
			}
		}
		if !d.IsDir() {
			if regex_p.MatchString(d.Name()) {
				mathesFiles = append(mathesFiles, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}
	jsonBytes, err := json.Marshal(mathesFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal results: %w", err)
	}
	return map[string]string{
		"Matches": string(jsonBytes),
	}, nil
}

// 获取文件系统相关工具集合
func GetFileSystemTools() []tool.Tool {
	pwdtool := function.NewFunctionTool(
		PWD,
		function.WithName(pwdToolName),
		function.WithDescription("Get the current working directory."),
	)
	cdtool := function.NewFunctionTool(
		CD,
		function.WithName(cdToolName),
		function.WithDescription("Change the current working directory."),
	)
	lstool := function.NewFunctionTool(
		LS,
		function.WithName(lsToolName),
		function.WithDescription("List the files and subdirectories in the specified directory."),
	)
	mkdirTool := function.NewFunctionTool(
		Mkdir,
		function.WithName(mkdirToolName),
		function.WithDescription("Create a directory, optionally creating parent directories recursively."),
	)
	copyTool := function.NewFunctionTool(
		Copy,
		function.WithName(cpToolName),
		function.WithDescription("Copy a file or directory, including across devices."),
	)
	mvTool := function.NewFunctionTool(
		MV,
		function.WithName(mvToolName),
		function.WithDescription("Move or rename a file or directory, including across devices."),
	)
	globTool := function.NewFunctionTool(
		Glob,
		function.WithName(globToolName),
		function.WithDescription("Search for file names by regular expression, with a configurable root directory and search depth."),
	)
	return []tool.Tool{pwdtool, cdtool, lstool, mkdirTool, copyTool, mvTool, globTool}
}
