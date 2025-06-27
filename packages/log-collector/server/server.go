package logcollector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.uber.org/zap"
)

type LogMeta struct {
	TraceID   string `json:"traceID"`
	SandboxID string `json:"sandboxID"`
	EnvID     string `json:"envID"`
	TeamID    string `json:"teamID"`
}

// make sure log dir already exists
func init() {
	if err := utils.CreateDirAllIfNotExists(consts.LogDiskDir, 0o755); err != nil {
		errMsg := fmt.Errorf("check log dir failed: %w", err)
		panic(errMsg)
	}
}

func EnvdLogHandler(w http.ResponseWriter, r *http.Request) {
	// for now only support POST method
	if r.Method != http.MethodPost {
		http.Error(w, "only allow post", http.StatusMethodNotAllowed)
		return
	}
	if r.Body == nil {
		http.Error(w, "expect a body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var meta LogMeta
	body, _ := io.ReadAll(r.Body)
	err := json.Unmarshal(body, &meta)
	if err != nil {
		errMsg := fmt.Errorf("error while parse body: %w", err)
		zap.L().Error("", zap.Error(errMsg))
		http.Error(w, errMsg.Error(), http.StatusBadRequest)
		return
	}
	file, err := os.OpenFile(
		filepath.Join(consts.LogDiskDir, meta.SandboxID+".log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		errMsg := fmt.Errorf("error while open log file: %w", err)
		zap.L().Error("", zap.Error(errMsg))
		http.Error(w, errMsg.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	if _, err = file.Write(body); err != nil {
		errMsg := fmt.Errorf("error write log file: %w", err)
		zap.L().Error("", zap.Error(errMsg), zap.String("sandbox-id", meta.SandboxID))
		http.Error(w, errMsg.Error(), http.StatusBadRequest)
		return
	}
	// write a line break
	if _, err = fmt.Fprint(file, "\n"); err != nil {
		errMsg := fmt.Errorf("error write log file: %w", err)
		zap.L().Error("", zap.Error(errMsg), zap.String("sandbox-id", meta.SandboxID))
		http.Error(w, errMsg.Error(), http.StatusBadRequest)
		return
	}
	zap.L().Info("save the log succeed!",
		zap.String("sandbox-id", meta.SandboxID),
		zap.Int("size", len(body)),
	)
	w.WriteHeader(http.StatusOK)
}
