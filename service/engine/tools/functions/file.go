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
	// Bytes 来自模型，设上限防止超大请求直接分配超大缓冲区；之后还会按序列化大小收缩。
	window := req.Bytes
	if window > modeltext.MaxInlineBytes {
		window = modeltext.MaxInlineBytes
	}
	var content []byte
	for {
		// 错误必须检查：Offset 非法时指针会停在 0，静默读回文件开头；窗口收缩重读后也要重新 Seek。
		if _, err := fd.Seek(int64(req.Offset), io.SeekStart); err != nil {
			return nil, fmt.Errorf("failed to seek to offset %d: %w", req.Offset, err)
		}
		buf := make([]byte, window)
		// Read 不保证填满缓冲区（short read 合法），文件被后台写入时会误判成 EOF；ReadFull 保证读满。EOF/ErrUnexpectedEOF 属正常。
		n, err := io.ReadFull(fd, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		// 按实际读取长度截取并对齐 rune 边界，去掉被窗口劈开的半个多字节字符；
		// 被去掉的半个字符下次从 NextOffset 读取时会重新取回，不丢数据。
		content = modeltext.TrimPartialRune(buf[:n])
		// 整个窗口都是二进制时不能再 trim：trim 到空会卡在同一 Offset 无限重读，原样返回，进度优先。
		if len(content) == 0 && n > 0 {
			content = buf[:n]
		}
		// 一页的体量按 JSON 序列化后的大小设防（非法字符序列化后会成倍膨胀，见 modeltext）：
		// 超限就窗口减半重读，NextOffset 分页语义不变；减到 1 字节时必不超限，循环必终止。
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

// searchInFileRequest 是 Grep 工具的请求；正则约束（RE2 语法等）见工具的 WithDescription。
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
	re_p, err := regexp.Compile(req.Regex) //不用MustCompile：失败时Panic，Compile返回error
	if err != nil {
		return nil, errors.New("invalid regex pattern")
	}
	contentBytes, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, err
	}
	// 匹配列表按 inline 预算设防：防止宽松正则把整个文件（序列化后放大数倍）塞进模型上下文，超限即截断并告知总数。
	m := map[string]string{}
	matches := re_p.FindAllIndex(contentBytes, -1)
	index := 0
	total := 0
	truncated := false
	for _, match := range matches {
		startOffset := match[0]
		endOffset := match[1]

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
		// 条目开销按最终序列化形式度量（值 + `"N":` 键 + 逗号），预算按完整开销累计，保证结果不超限。
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

// 获取文件操作工具集合（WriteFile/ReadFile/EditFile/Grep/DeleteFile/FileStat/Diff）。
const (
	writeFileToolName    string = "WriteFile"
	readFileToolName     string = "ReadFile"
	editFileToolName     string = "EditFile"
	grepToolName string = "Grep"
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
		function.WithName(grepToolName),
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
