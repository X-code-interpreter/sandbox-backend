package process

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/e2b-dev/infra/packages/envd/internal/user"
	"go.uber.org/zap"
)

type SimpleProcess struct {
	cmd     *exec.Cmd
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	exit_ch <-chan int
}

type SimpleProcessManager struct {
	mu        sync.Mutex
	processes map[int]*SimpleProcess
	logger    *zap.SugaredLogger
}

type SimpleProcessCreateRequest struct {
	Cmd  string            `json:"cmd"`
	User string            `json:"user,omitempty"`
	Envs map[string]string `json:"envs,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
}

type SimpleProcessCreateResponse struct {
	Pid int `json:"pid"`
}

type SimpleProcessWaitRequest struct {
	Pid int `json:"pid"`
}

type SimpleProcessWaitResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

type SimpleProcessKillRequest struct {
	Pid int `json:"pid"`
}

func NewSimpleProcessManager(logger *zap.SugaredLogger) *SimpleProcessManager {
	return &SimpleProcessManager{
		processes: make(map[int]*SimpleProcess),
		logger:    logger,
	}
}

func (m *SimpleProcessManager) getProc(pid int) *SimpleProcess {
	m.mu.Lock()
	defer m.mu.Unlock()
	if proc, exist := m.processes[pid]; exist {
		return proc
	} else {
		return nil
	}
}

func (m *SimpleProcessManager) putProc(proc *SimpleProcess) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pid := proc.cmd.Process.Pid
	if _, exist := m.processes[pid]; !exist {
		m.processes[pid] = proc
		return nil
	} else {
		return fmt.Errorf("process with pid %d already exists", pid)
	}
}

func (m *SimpleProcessManager) delProc(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processes, pid)
}

func create(req *SimpleProcessCreateRequest, logger *zap.SugaredLogger) (*SimpleProcess, error) {
	cmd := exec.Command("/bin/bash", "-l", "-c", req.Cmd)
	userName := user.DefaultUser
	if len(req.User) > 0 {
		userName = req.User
	}
	uid, gid, homedir, username, err := user.GetUser(userName)
	if err != nil {
		return nil, fmt.Errorf("error getting user '%s': %w", user.DefaultUser, err)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{uint32(gid)}, NoSetGroups: true}

	if req.Cwd == "" {
		cmd.Dir = homedir
	} else {
		cmd.Dir = req.Cwd
	}
	// We inherit the env vars from the root process, but we should handle this differently in the future.
	formattedVars := os.Environ()

	formattedVars = append(formattedVars, "HOME="+homedir)
	formattedVars = append(formattedVars, "USER="+username)
	formattedVars = append(formattedVars, "LOGNAME="+username)

	// Only the last values of the env vars are used - this allows for overwriting defaults
	for key, value := range req.Envs {
		formattedVars = append(formattedVars, key+"="+value)
	}

	cmd.Env = formattedVars

	exit_ch := make(chan int, 1)
	proc := &SimpleProcess{
		cmd:     cmd,
		exit_ch: exit_ch,
	}
	cmd.Stdout = &proc.stdout
	cmd.Stderr = &proc.stderr

	if err = cmd.Start(); err != nil {
		return proc, err
	}

	go func() {
		if err := cmd.Wait(); err != nil {
			logger.Errorw("Failed to wait for process", "processID", cmd.Process.Pid, "error", err)
		}
		exit_ch <- cmd.ProcessState.ExitCode()
		close(exit_ch)
	}()

	return proc, nil
}

// This is a simple process handler.
// Unlike the rpc one, this try to invovle minimal overhead in envd.
func (m *SimpleProcessManager) Create(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		decoder := json.NewDecoder(r.Body)
		var req SimpleProcessCreateRequest
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p, err := create(&req, m.logger)
		if err != nil {
			http.Error(w, fmt.Sprintf("create process failed: %s", err), http.StatusInternalServerError)
			return
		}
		if err := m.putProc(p); err != nil {
			http.Error(w, fmt.Sprintf("create process failed: %s", err), http.StatusInternalServerError)
			return
		}

		response := SimpleProcessCreateResponse{Pid: p.cmd.Process.Pid}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("encode response failed: %s", err), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func (m *SimpleProcessManager) Wait(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		decoder := json.NewDecoder(r.Body)
		var req SimpleProcessWaitRequest
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p := m.getProc(req.Pid)
		if p == nil {
			http.Error(w, fmt.Sprintf("process not found: %d", req.Pid), http.StatusInternalServerError)
			return
		}
		exitCode := <-p.exit_ch

		response := SimpleProcessWaitResponse{
			ExitCode: exitCode,
			Stdout:   p.stdout.String(),
			Stderr:   p.stderr.String(),
		}
		m.delProc(req.Pid)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("encode response failed: %s", err), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func (m *SimpleProcessManager) Kill(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		decoder := json.NewDecoder(r.Body)
		var req SimpleProcessKillRequest
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p := m.getProc(req.Pid)
		if p == nil {
			http.Error(w, fmt.Sprintf("process not found: %d", req.Pid), http.StatusInternalServerError)
			return
		}
		if err := p.cmd.Process.Kill(); err != nil {
			http.Error(w, fmt.Sprintf("send kill to process %d failed: %s", req.Pid, err), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}
