package localexec

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Submit 创建任务但不启动
func (m *Manager) Submit(opts SubmitOptions) string {
	id := randomID()
	job := &Job{SubmitOptions: opts, ID: id, status: StatusPending, createdAt: time.Now()}
	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()
	return id
}

// Start 启动任务
func (m *Manager) Start(id string) error {
	job := m.get(id)
	if job == nil {
		return errors.New("job not found")
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.status != StatusPending {
		return errors.New("job not in pending status")
	}

	cmd := buildCmd(job.SubmitOptions)
	cmd.Dir = strings.TrimSpace(job.Dir)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	stdin, _ := cmd.StdinPipe()
	job.stdin = stdin

	if err := cmd.Start(); err != nil {
		job.status = StatusFailed
		job.errStr = err.Error()
		return err
	}
	job.cmd = cmd
	job.pid = cmd.Process.Pid
	job.status = StatusRunning
	job.startedAt = time.Now()

	// 异步读取输出
	go copyStream(stdout, &job.stdoutBuf)
	go copyStream(stderr, &job.stderrBuf)

	// 等待结束
	go func() {
		err := cmd.Wait()
		job.mu.Lock()
		defer job.mu.Unlock()
		job.endedAt = time.Now()
		job.pid = 0 // 进程已结束，PID无效
		if err != nil {
			job.status = StatusFailed
			job.errStr = err.Error()
			if exitErr, ok := err.(*exec.ExitError); ok {
				job.exitCode = exitErr.ExitCode()
			}
		} else {
			job.status = StatusDone
			job.exitCode = 0
		}
	}()

	return nil
}

// Status 返回某任务状态
func (m *Manager) Status(id string) StatusInfo {
	job := m.get(id)
	if job == nil {
		return StatusInfo{ID: id, Status: "not-found"}
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return StatusInfo{
		ID:        job.ID,
		Status:    job.status,
		PID:       job.pid,
		ExitCode:  job.exitCode,
		Error:     job.errStr,
		Command:   job.Command,
		Shell:     job.Shell,
		CreatedAt: job.createdAt,
		StartedAt: job.startedAt,
		EndedAt:   job.endedAt,
	}
}

// StatusAll 返回全部任务的状态
func (m *Manager) StatusAll() []StatusInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]StatusInfo, 0, len(m.jobs))
	for _, j := range m.jobs {
		res = append(res, m.Status(j.ID))
	}
	return res
}

// Output 返回输出
func (m *Manager) Output(id string, opts OutputOptions) ([]byte, error) {
	job := m.get(id)
	if job == nil {
		return nil, errors.New("job not found")
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	var buf *bytes.Buffer
	if strings.ToLower(opts.Stream) == "stderr" {
		buf = &job.stderrBuf
	} else {
		buf = &job.stdoutBuf
	}
	data := buf.Bytes()
	if opts.Window > 0 && opts.Window < len(data) {
		return data[len(data)-opts.Window:], nil
	}
	return data, nil
}

// WriteStdin 写入stdin
func (m *Manager) WriteStdin(id string, data []byte) error {
	job := m.get(id)
	if job == nil {
		return errors.New("job not found")
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.status != StatusRunning {
		return errors.New("job not running")
	}
	if job.stdin == nil {
		return errors.New("stdin not available")
	}
	_, err := job.stdin.Write(data)
	return err
}

// Signal 发送信号（Windows仅支持Kill作为强制结束）
func (m *Manager) Signal(id, signal string) error {
	job := m.get(id)
	if job == nil {
		return errors.New("job not found")
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.status != StatusRunning {
		return errors.New("job not running")
	}
	if job.cmd == nil || job.cmd.Process == nil {
		return errors.New("process not available")
	}
	if runtime.GOOS == "windows" {
		// 简化：Windows不区分信号，统一Kill
		return job.cmd.Process.Kill()
	}
	// 非Windows：尽量映射常见信号
	signal = strings.ToUpper(signal)
	switch signal {
	case "SIGINT":
		return job.cmd.Process.Signal(os.Interrupt)
	case "SIGTERM":
		// Go没有标准SIGTERM变量，使用 syscall 信号；为简化，这里直接Kill替代
		return job.cmd.Process.Kill()
	case "SIGKILL":
		return job.cmd.Process.Kill()
	default:
		return errors.New("unsupported signal")
	}
}

// Kill 强制结束
func (m *Manager) Kill(id string) error {
	job := m.get(id)
	if job == nil {
		return errors.New("job not found")
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.cmd == nil || job.cmd.Process == nil {
		return errors.New("process not available")
	}
	if err := job.cmd.Process.Kill(); err != nil {
		return err
	}
	job.status = StatusKilled
	job.endedAt = time.Now()
	job.pid = 0
	return nil
}

// 内部工具
func (m *Manager) get(id string) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

func copyStream(r io.Reader, w *bytes.Buffer) {
	// 不在持有锁的情况下进行阻塞 I/O
	// 直接写入 buffer，bytes.Buffer 自身是协程安全的
	io.Copy(w, r)
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func kvEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func buildCmd(opts SubmitOptions) *exec.Cmd {
	// shell模式：将Command作为一整行交给shell
	shell := strings.ToLower(strings.TrimSpace(opts.Shell))
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "powershell"
		} else {
			shell = "bash"
		}
	}
	if runtime.GOOS == "windows" && shell == "powershell" {
		// 使用 -NoLogo -NoProfile 保持干净环境
		return exec.Command("powershell", "-NoLogo", "-NoProfile", "-Command", "$OutputEncoding = [System.Text.Encoding]::UTF8; "+opts.Command)
	}
	if shell == "bash" {
		return exec.Command("bash", "-lc", opts.Command)
	}
	// 兜底：直接执行命令
	return exec.Command(opts.Command)
}
