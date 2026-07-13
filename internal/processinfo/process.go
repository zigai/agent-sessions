package processinfo

import "fmt"

// Process is a point-in-time snapshot of a process. Args is transient inventory
// data and must not be persisted by callers.
type Process struct {
	PID            int
	PPID           int
	ProcessGroupID int
	StartIdentity  string
	Executable     string
	CWD            string
	TTY            string
	Args           []string `json:"-"`
}

// UnsupportedError reports that process enumeration is unavailable on the
// current operating system.
type UnsupportedError struct {
	Platform string
}

func (e *UnsupportedError) Error() string {
	if e.Platform == "" {
		return "process enumeration is unsupported"
	}
	return "process enumeration is unsupported on " + e.Platform
}

// PermissionError reports an inability to inspect a process table or entry.
type PermissionError struct {
	Path string
	Err  error
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("permission denied reading process information at %s: %v", e.Path, e.Err)
}

func (e *PermissionError) Unwrap() error { return e.Err }

// TableError reports malformed or otherwise unusable process table data.
type TableError struct {
	Path string
	Err  error
}

func (e *TableError) Error() string {
	return fmt.Sprintf("invalid process table data at %s: %v", e.Path, e.Err)
}

func (e *TableError) Unwrap() error { return e.Err }
