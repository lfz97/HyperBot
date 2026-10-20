package functionTools

import (
	"HyperBot/service/engine/tools/modeltext"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pmezard/go-difflib/difflib"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func WriteFile(ctx context.Context, req struct {
	Path    string `json:"Path" jsonschema:"description=Path of the file to write."`
	Content string `json:"Content" jsonschema:"description=Content to write into the file."`
	Append  bool   `json:"Append" jsonschema:"description=Enable append mode. Defaults to false which overwrites the whole file; true appends to the end of the file."`
}) (map[string]string, error) {

	if req.Path == "" {
		return nil, errors.New("`Path` cannot be empty")
	}
	var fd *os.File
	var err error
	if req.Append {
		fd, err = os.OpenFile(req.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		defer fd.Close()
	} else {
		fd, err = os.OpenFile(req.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return nil, err
		}
		defer fd.Close()
	}

	length, err := fd.WriteString(req.Content)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"BytesWritten": strconv.Itoa(length),
		"content":      req.Content,
	}, nil
}

type readFileRequest struct {
	Path   string `json:"Path" jsonschema:"description=Path of the file to read."`
	Bytes  int    `json:"Bytes" jsonschema:"description=Read window size in bytes. Defaults to 1024."`
	Offset int    `json:"Offset" jsonschema:"description=Byte offset to start reading from. Defaults to 0 which reads from the beginning of the file."`
}

func ReadFile(ctx context.Context, req readFileRequest) (map[string]string, error) {

	if req.Path == "" {
		return nil, errors.New("`Path` cannot be empty")
	}
	if req.Bytes < 0 {
		return nil, errors.New("`bytes` must >= 0")
	}
	if req.Offset < 0 {
		return nil, errors.New("`Offset` must >= 0")
	}
	if req.Bytes == 0 {
		req.Bytes = 1024
	}
	fd, err := os.OpenFile(req.Path, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	fi, err := fd.Stat()
	if err != nil {
		return nil, err
	}
	total := fi.Size()
	// 窗口上限同时约束缓冲区分配：Bytes 是模型给的，不设防的话一个超大请求会
	// 直接按它分配缓冲区。窗口随后还会按序列化大小收缩（见下）。
	window := req.Bytes
	if window > modeltext.MaxInlineBytes {
		window = modeltext.MaxInlineBytes
	}
	var content []byte
	for {
		// Seek 的错误必须检查：Offset 非法时若忽略返回值，文件指针会停在 0，
		// 随后读到的还是文件开头——调用方以为读的是自己指定的位置，拿到错数据
		// 却毫无提示。窗口收缩后重读也要重新 Seek。
		if _, err := fd.Seek(int64(req.Offset), io.SeekStart); err != nil {
			return nil, fmt.Errorf("failed to seek to offset %d: %w", req.Offset, err)
		}
		buf := make([]byte, window) // 根据窗口大小创建缓冲区
		// 用 ReadFull 而不是单次 Read：Read 不保证填满缓冲区（short read 是合法的），
		// 文件正被后台任务写入时会被误判成 EOF。读到文件末尾时 EOF/ErrUnexpectedEOF 属正常。
		n, err := io.ReadFull(fd, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		// 按实际读取长度截取，否则窗口大于剩余内容时会带出多余的空字节填充；
		// 再对齐到 rune 边界，去掉被字节窗口劈开的半个多字节字符（中文输出尤其明显）。
		// 被去掉的半个字符会在下次从 NextOffset 读取时重新取回，不丢数据。
		content = modeltext.TrimPartialRune(buf[:n])
		// 读到了字节但整个窗口都不是合法 UTF-8（二进制文件尾部很常见）时不能再 trim：
		// 否则 content 为空、NextOffset 原地不动、EOF 又是 false，调用方会卡在同一
		// Offset 上无限重读。这种情况下原样返回，进度优先于编码整洁。
		if len(content) == 0 && n > 0 {
			content = buf[:n]
		}
		// Content 经 JSON 序列化后才发给模型，非法 UTF-8/控制字符会成倍膨胀
		//（见 modeltext），所以一页的体量按序列化后的大小设防：超限就把窗口
		// 减半重读，分页语义不变（NextOffset 始终指向未读位置，数据不丢，只是
		// 这页变小）。窗口收到 1 字节时序列化后必不超限，循环必然终止。
		if window == 1 || modeltext.Size(string(content)) <= modeltext.MaxInlineBytes {
			break
		}
		window /= 2
	}
	next := req.Offset + len(content)
	return map[string]string{
		"ReadPath":   req.Path,
		"ReadLength": strconv.Itoa(len(content)),
		"NextOffset": strconv.Itoa(next), //分页续读时直接把这个值传给 Offset，无需自己计算
		"TotalSize":  strconv.Itoa(int(total)),
		"EOF":        strconv.FormatBool(int64(next) >= total),
		"Content":    string(content),
	}, nil
}

// EditFile：编辑指定文件中的内容，支持替换指定的旧内容为新内容。默认仅允许唯一匹配时替换（多处匹配会报错），设置replace_all为true则全量替换。
func EditFile(ctx context.Context, req struct {
	Path       string `json:"Path" jsonschema:"description=Path of the file to edit."`
	Old        string `json:"Old" jsonschema:"description=The old content to be replaced."`
	New        string `json:"New" jsonschema:"description=The new content to replace it with."`
	ReplaceAll bool   `json:"ReplaceAll" jsonschema:"description=Replace every match in the file. Defaults to false which only allows a unique match (multiple matches return an error); set true to replace all occurrences."`
}) (map[string]string, error) {
	if req.Path == "" {
		return nil, errors.New("`Path` cannot be empty")
	}
	if req.Old == "" {
		return nil, errors.New("`Old` cannot be empty")
	}
	oldBytes, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, err
	}
	oldContent := string(oldBytes)

	Indexes := []int{}
	offset := 0
	for {
		idx := strings.Index(oldContent[offset:], req.Old)
		if idx == -1 {
			break
		}
		offset += idx
		Indexes = append(Indexes, offset)
		offset += len(req.Old)
	}
	if len(Indexes) == 0 {
		return nil, errors.New("oldContent not found in file")
	}
	if !req.ReplaceAll && len(Indexes) > 1 {
		return nil, errors.New("multiple matches found for oldContent, but replace_all is set to false")
	}

	newContent := strings.ReplaceAll(oldContent, req.Old, req.New)
	err = os.WriteFile(req.Path, []byte(newContent), 0644)
	if err != nil {
		return nil, err
	}

	// 生成 unified diff，方便 LLM 验证编辑结果
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldContent),
		B:        difflib.SplitLines(newContent),
		FromFile: req.Path,
		ToFile:   req.Path,
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Diff": text,
	}, nil
}

type matchInfo struct {
	StartlineNum int    `json:"StartLineNum"`
	EndlineNum   int    `json:"EndLineNum"`
	MatchContent string `json:"MatchContent"`
}

// 通过正则表达式在指定文件中搜索内容，返回所有匹配项的行号和内容。使用Go RE2语法，不支持lookahead/lookbehind/backreference。`.`默认不匹配换行，跨行匹配用`(?s)`。`^`和`$`默认匹配文本首尾，匹配行首行尾用`(?m)`。
type searchInFileRequest struct {
	Path  string `json:"Path" jsonschema:"description=Path of the file to search in."`
	Regex string `json:"Regex" jsonschema:"description=The regular expression to search for."`
}

func SearchInFile(ctx context.Context, req searchInFileRequest) (map[string]string, error) {
	if req.Path == "" {
		return nil, errors.New("`Path` cannot be empty")
	}
	if req.Regex == "" {
		return nil, errors.New("`Regex` cannot be empty")
	}
	re_p, err := regexp.Compile(req.Regex) //不使用MustCompile，因为MustCompile失败时会直接Panic，Compile是返回error
	if err != nil {
		return nil, errors.New("invalid regex pattern")
	}
	contentBytes, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, err
	}
	// 匹配列表无上限的话，一个宽松的正则就能把整个文件（序列化后再放大数倍）
	// 塞进模型上下文。按 modeltext 的内联预算逐条累计，超限即截断并告知总数，
	// 剩余的匹配让 agent 收窄正则或用 ReadFile 分页去取。
	m := map[string]string{}
	matches := re_p.FindAllIndex(contentBytes, -1)
	index := 0
	total := 0
	truncated := false
	for _, match := range matches {
		startOffset := match[0]
		endOffset := match[1]

		// \n出现的次数加1即为行号
		startlineNum := strings.Count(string(contentBytes[:startOffset]), "\n") + 1
		endlineNum := strings.Count(string(contentBytes[:endOffset]), "\n") + 1

		matchContent_b := contentBytes[startOffset:endOffset]
		info := matchInfo{
			StartlineNum: startlineNum,
			EndlineNum:   endlineNum,
			MatchContent: string(matchContent_b),
		}
		infoBytes, err := json.Marshal(info)
		if err != nil {
			return nil, err
		}
		// 值是内层 JSON 的字符串形式，外层序列化时引号还会再转义，所以按最终形式
		// 度量：条目的完整开销 = 值 + `"N":` 键与冒号 + 逗号分隔符。预算按完整
		// 开销累计，保证最终序列化结果贴近预算而不明显超出。
		cost := modeltext.Size(strconv.Itoa(index)) + 2 + modeltext.Size(string(infoBytes))
		if total+cost > modeltext.MaxInlineBytes {
			truncated = true
			break
		}
		m[strconv.Itoa(index)] = string(infoBytes)
		total += cost
		index++
	}
	if truncated {
		m["Truncated"] = "true"
		m["TotalMatches"] = strconv.Itoa(len(matches))
		m["Note"] = fmt.Sprintf(
			"Result truncated at the %d-byte inline budget after %d matches; %d matches in total. "+
				"Narrow the regex or read the file with ReadFile instead.",
			modeltext.MaxInlineBytes, index, len(matches))
	}
	return m, nil
}

func DeleteFile(ctx context.Context, req struct {
	Path string `json:"Path" jsonschema:"description=Path of the file or directory to delete."`
}) (map[string]string, error) {
	if req.Path == "" {
		return nil, errors.New("`Path` cannot be empty")
	}
	err := os.RemoveAll(req.Path)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Deleted": req.Path,
	}, nil
}

func FileInfo(ctx context.Context, req struct {
	Path string `json:"Path" jsonschema:"description=Path of the file or directory to inspect."`
}) (map[string]string, error) {
	if req.Path == "" {
		return nil, errors.New("`Path` cannot be empty")
	}
	info, err := os.Stat(req.Path)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Name":     info.Name(),
		"Size":     strconv.FormatInt(info.Size(), 10),
		"IsDir":    strconv.FormatBool(info.IsDir()),
		"Mode":     info.Mode().String(),
		"ModeTime": info.ModTime().String(),
	}, nil
}

func Diff(ctx context.Context, req struct {
	PathA string `json:"PathA" jsonschema:"description=Path of the first file to compare."`
	PathB string `json:"PathB" jsonschema:"description=Path of the second file to compare."`
}) (map[string]string, error) {
	if req.PathA == "" || req.PathB == "" {
		return nil, errors.New("`PathA` and `PathB` cannot be empty")
	}
	if req.PathA == req.PathB {
		return map[string]string{
			"Message": "The two paths are the same, no differences.",
		}, nil
	}
	fileA_bytes, err := os.ReadFile(req.PathA)
	if err != nil {
		return nil, err
	}
	fileB_bytes, err := os.ReadFile(req.PathB)
	if err != nil {
		return nil, err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(fileA_bytes)),
		B:        difflib.SplitLines(string(fileB_bytes)),
		FromFile: filepath.Base(req.PathA),
		ToFile:   filepath.Base(req.PathB),
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Diff": text,
	}, nil
}

// 获取文件操作工具集合：
// WriteFile：将内容写入指定文件，如果文件不存在则创建，已存在则覆盖。
// ReadFile：从指定文件读取内容，支持设置读取窗口大小。
const (
	writeFileToolName    string = "WriteFile"
	readFileToolName     string = "ReadFile"
	editFileToolName     string = "EditFile"
	searchInFileToolName string = "SearchInFile"
	deleteFileToolName   string = "DeleteFile"
	fileStatToolName     string = "FileStat"
	diffToolName         string = "Diff"
)

func GetFileOperationsTools() []tool.Tool {
	wftool := function.NewFunctionTool(
		WriteFile,
		function.WithName(writeFileToolName),
		function.WithDescription("Write content to the specified file. The file is created if it does not exist and overwritten if it does."),
	)
	rftool := function.NewFunctionTool(
		ReadFile,
		function.WithName(readFileToolName),
		function.WithDescription("Read content from the specified file, with a configurable read window size and byte offset. The page size is capped at the inline budget: if the requested window would exceed it after JSON serialization (e.g. binary or control characters), the window is shrunk and the returned page is smaller than requested; use NextOffset to continue reading."),
	)
	eftool := function.NewFunctionTool(
		EditFile,
		function.WithName(editFileToolName),
		function.WithDescription("Edit the specified file by replacing old content with new content. By default only a unique match may be replaced (multiple matches return an error); set replace_all to true to replace every occurrence."),
	)
	sftool := function.NewFunctionTool(
		SearchInFile,
		function.WithName(searchInFileToolName),
		function.WithDescription("Search the specified file with a regular expression and return the line numbers and content of all matches. Uses Go RE2 syntax: lookahead, lookbehind and backreference are not supported. `.` does not match newlines by default; use `(?s)` to match across lines. `^` and `$` match the start and end of the whole text by default; use `(?m)` to match line starts and ends. The result is capped at the inline budget: when truncated, `Truncated` is true and `TotalMatches` reports the total count."),
	)
	dftool := function.NewFunctionTool(
		DeleteFile,
		function.WithName(deleteFileToolName),
		function.WithDescription("Delete the specified file or directory. Directories are removed recursively, so use this with caution."),
	)
	fitool := function.NewFunctionTool(
		FileInfo,
		function.WithName(fileStatToolName),
		function.WithDescription("Get information about the specified file or directory, including name, size, whether it is a directory, permission mode and modification time."),
	)
	difftool := function.NewFunctionTool(
		Diff,
		function.WithName(diffToolName),
		function.WithDescription("Compare two files and return the differences in unified diff format."),
	)
	return []tool.Tool{wftool, rftool, eftool, sftool, dftool, fitool, difftool}
}

// trimPartialRune 已上收到 modeltext 包（TrimPartialRune），与 localexec 的落盘
// 预览共用同一份实现。
