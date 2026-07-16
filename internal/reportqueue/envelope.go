package reportqueue

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	EnvelopeVersion = 2
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

// Report is the v2 observation payload carried by a queue envelope.
//
// It aliases the registry contract so queue serialization cannot drift from
// registry observation semantics.
type Report = registry.Observation

func ReportFromRegistry(observation registry.Observation) Report {
	return observation
}

func RegistryObservation(report Report) registry.Observation {
	return report
}

func RegistryReport(report Report) registry.Observation {
	return report
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

func (q Queue) tmuxCacheLockPath() string {
	return filepath.Join(q.root, "tmux-cache.lock")
}

func NewEnvelopeID(now time.Time) string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("%s-%d", now.UTC().Format("20060102T150405.000000000Z"), os.Getpid())
	}

	return fmt.Sprintf("%s-%s", now.UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(random[:]))
}
