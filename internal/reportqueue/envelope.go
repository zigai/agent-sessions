package reportqueue

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const (
	EnvelopeVersion = 1
	KindReport      = "report"
)

type Envelope struct {
	Version             int                  `json:"version"`
	ID                  string               `json:"id"`
	CreatedAt           time.Time            `json:"created_at"`
	StorePath           string               `json:"store_path"`
	Kind                string               `json:"kind"`
	Report              Report               `json:"report"`
	RawPayloadSet       bool                 `json:"raw_payload_set,omitempty"`
	NoTmux              bool                 `json:"no_tmux,omitempty"`
	Stdin               []byte               `json:"stdin_base64,omitempty"`
	Runtime             RuntimeContext       `json:"runtime"`
	Attempt             int                  `json:"attempt,omitempty"`
	NextAttemptAt       time.Time            `json:"next_attempt_at,omitzero"`
	ProcessingStartedAt time.Time            `json:"processing_started_at,omitzero"`
	WorkerPID           int                  `json:"worker_pid,omitempty"`
	LastError           string               `json:"last_error,omitempty"`
	CachedTmux          registry.TmuxContext `json:"cached_tmux,omitzero"`
}

type Report struct {
	Harness          registry.Harness     `json:"harness"`
	State            registry.State       `json:"state"`
	SessionID        string               `json:"session_id,omitempty"`
	SessionPath      string               `json:"session_path,omitempty"`
	ResumeCommand    []string             `json:"resume_command,omitempty"`
	CWD              string               `json:"cwd,omitempty"`
	ProjectRoot      string               `json:"project_root,omitempty"`
	PID              int                  `json:"pid,omitempty"`
	ProcessStartTime string               `json:"process_start_time,omitempty"`
	PPID             int                  `json:"ppid,omitempty"`
	TTY              string               `json:"tty,omitempty"`
	Tmux             registry.TmuxContext `json:"tmux,omitzero"`
	Source           string               `json:"source,omitempty"`
	Confidence       string               `json:"confidence,omitempty"`
	Event            string               `json:"event,omitempty"`
	Attributes       map[string]string    `json:"attributes,omitempty"`
	RawPayload       json.RawMessage      `json:"raw_payload,omitempty"`
	ObservedAt       time.Time            `json:"observed_at,omitzero"`
}

func ReportFromRegistry(report registry.Report) Report {
	return Report{
		Harness:          report.Harness,
		State:            report.State,
		SessionID:        report.SessionID,
		SessionPath:      report.SessionPath,
		ResumeCommand:    report.ResumeCommand,
		CWD:              report.CWD,
		ProjectRoot:      report.ProjectRoot,
		PID:              report.PID,
		ProcessStartTime: report.ProcessStartTime,
		PPID:             report.PPID,
		TTY:              report.TTY,
		Tmux:             report.Tmux,
		Source:           report.Source,
		Confidence:       report.Confidence,
		Event:            report.Event,
		Attributes:       report.Attributes,
		RawPayload:       report.RawPayload,
		ObservedAt:       report.ObservedAt,
	}
}

func (r Report) RegistryReport() registry.Report {
	return registry.Report{
		Harness:          r.Harness,
		State:            r.State,
		SessionID:        r.SessionID,
		SessionPath:      r.SessionPath,
		ResumeCommand:    r.ResumeCommand,
		CWD:              r.CWD,
		ProjectRoot:      r.ProjectRoot,
		PID:              r.PID,
		ProcessStartTime: r.ProcessStartTime,
		PPID:             r.PPID,
		TTY:              r.TTY,
		Tmux:             r.Tmux,
		Source:           r.Source,
		Confidence:       r.Confidence,
		Event:            r.Event,
		Attributes:       r.Attributes,
		RawPayload:       r.RawPayload,
		ObservedAt:       r.ObservedAt,
	}
}

type RuntimeContext struct {
	CWD        string            `json:"cwd,omitempty"`
	ParentArgs []string          `json:"parent_args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

type Queue struct {
	root string
}

func New(storePath string) Queue {
	return Queue{root: RootForStore(storePath)}
}

func RootForStore(storePath string) string {
	storePath = strings.TrimSpace(storePath)
	if storePath == "" {
		storePath = registry.DefaultStorePath()
	}
	if abs, err := filepath.Abs(storePath); err == nil {
		storePath = abs
	}
	sum := sha256.Sum256([]byte(filepath.Clean(storePath)))
	storeHash := hex.EncodeToString(sum[:8])

	return filepath.Join(filepath.Dir(storePath), ".agent-sessions-queue", storeHash)
}

func (q Queue) Root() string {
	return q.root
}

func (q Queue) pendingDir() string {
	return filepath.Join(q.root, "pending")
}

func (q Queue) processingDir() string {
	return filepath.Join(q.root, "processing")
}

func (q Queue) deadDir() string {
	return filepath.Join(q.root, "dead")
}

func (q Queue) lockPath() string {
	return filepath.Join(q.root, "drain.lock")
}

func NewEnvelopeID(now time.Time) string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("%s-%d", now.UTC().Format("20060102T150405.000000000Z"), os.Getpid())
	}

	return fmt.Sprintf("%s-%s", now.UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(random[:]))
}
