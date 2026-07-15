package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	storeArgument = "--store"
	jsonArgument  = "--json"
)

type fastPathGlobals struct {
	storePath  string
	outputJSON bool
}
type fastPathFlag struct {
	name, inlineValue string
	hasInlineValue    bool
}

var (
	errFastPathMissingValue      = errors.New("missing value")
	errFastPathTooManyReportArgs = errors.New("too many report arguments")
	errFastPathRequiresHarness   = errors.New("requires a harness")
)

func tryExecuteFastPath(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, error) {
	g, c, a, ok := splitFastPathArgs(args)
	if !ok {
		return false, nil
	}
	app := &application{storePath: g.storePath, outputJSON: g.outputJSON, stdout: stdout, stderr: stderr}
	switch c {
	case reportCommandName:
		o, ok, e := parseFastReportOptions(a)
		if !ok || e != nil {
			return ok, e
		}
		return true, app.runReport(ctx, stdin, o)
	case hookCommandName, agyHookCommandName:
		h, o, ok, e := parseFastManagedHookOptions(c, a)
		if !ok || e != nil {
			return ok, e
		}
		return true, app.runManagedHook(ctx, stdin, h, o)
	}
	return false, nil
}

func splitFastPathArgs(args []string) (fastPathGlobals, string, []string, bool) {
	var g fastPathGlobals
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == storeArgument:
			if i+1 >= len(args) {
				return g, "", nil, false
			}
			i++
			g.storePath = args[i]
		case strings.HasPrefix(a, storeArgument+"="):
			g.storePath = strings.TrimPrefix(a, storeArgument+"=")
		case a == jsonArgument:
			g.outputJSON = true
		case strings.HasPrefix(a, "-"):
			return g, "", nil, false
		case a == reportCommandName || a == hookCommandName || a == agyHookCommandName:
			return g, a, args[i+1:], true
		default:
			return g, "", nil, false
		}
	}
	return g, "", nil, false
}

type fastReportParseState struct {
	o                              reportOptions
	positionals                    []string
	cwdChanged, projectRootChanged bool
}

//nolint:gocognit,cyclop // fast-path parsing mirrors the public command grammar
func parseFastReportOptions(args []string) (reportOptions, bool, error) {
	s := fastReportParseState{o: defaultReportOptionsFromEnv()}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			s.positionals = append(s.positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			s.positionals = append(s.positionals, a)
			continue
		}
		f := newFastPathFlag(a)
		var v string
		var e error
		need := func() (string, error) {
			if f.hasInlineValue {
				return f.inlineValue, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%w for %s", errFastPathMissingValue, f.name)
			}
			i++
			return args[i], nil
		}
		switch f.name {
		case "--harness":
			v, e = need()
			s.o.harness = v
		case "--presence":
			v, e = need()
			s.o.presence = v
		case "--activity":
			v, e = need()
			s.o.activity = v
		case "--session-id":
			v, e = need()
			s.o.sessionID = v
		case "--session-path":
			v, e = need()
			s.o.sessionPath = v
		case "--cwd":
			v, e = need()
			s.o.cwd = v
			s.cwdChanged = true
		case "--project-root":
			v, e = need()
			s.o.projectRoot = v
			s.projectRootChanged = true
		case "--tty":
			v, e = need()
			s.o.tty = v
		case "--start-identity":
			v, e = need()
			s.o.startIdentity = v
		case "--executable":
			v, e = need()
			s.o.executable = v
		case "--event":
			v, e = need()
			s.o.event = v
		case "--observed-at":
			v, e = need()
			s.o.observedAt = v
		case "--attribute":
			v, e = need()
			s.o.attributes = append(s.o.attributes, v)
		case "--resume-command":
			v, e = need()
			s.o.resumeCommand = append(s.o.resumeCommand, v)
		case "--pid":
			v, e = need()
			if e == nil {
				s.o.pid, e = strconv.Atoi(v)
			}
		case "--ppid":
			v, e = need()
			if e == nil {
				s.o.ppid, e = strconv.Atoi(v)
			}
		case "--process-group-id":
			v, e = need()
			if e == nil {
				s.o.processGroupID, e = strconv.Atoi(v)
			}
		case "--raw-stdin":
			if f.hasInlineValue {
				return s.o, false, nil
			}
			s.o.rawStdin = true
		case "--raw-stdin-defaults-only":
			if f.hasInlineValue {
				return s.o, false, nil
			}
			s.o.rawDefaultsOnly = true
		case "--no-tmux":
			if f.hasInlineValue {
				return s.o, false, nil
			}
			s.o.noTmux = true
		case "--queue":
			if f.hasInlineValue {
				return s.o, false, nil
			}
			s.o.queue = true
		case "--quiet":
			if f.hasInlineValue {
				return s.o, false, nil
			}
			s.o.quiet = true
		default:
			return s.o, false, nil
		}
		if e != nil {
			return s.o, true, fmt.Errorf("parsing %s: %w", f.name, e)
		}
	}
	if len(s.positionals) > 1 {
		return s.o, true, fmt.Errorf("%w: accepts one harness", errFastPathTooManyReportArgs)
	}
	if len(s.positionals) == 1 {
		s.o.harness = s.positionals[0]
	}
	if s.cwdChanged {
		s.o.cwdAuto = false
	}
	if s.projectRootChanged {
		s.o.projectRootAuto = false
	}
	return s.o, true, nil
}

func newFastPathFlag(arg string) fastPathFlag {
	n, v, h := strings.Cut(arg, "=")
	return fastPathFlag{name: n, inlineValue: v, hasInlineValue: h}
}

//nolint:cyclop // fast-path parsing validates each flag form explicitly
func parseFastManagedHookOptions(command string, args []string) (string, managedHookOptions, bool, error) {
	var o managedHookOptions
	h := ""
	if command == agyHookCommandName {
		h = "agy"
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			if command == hookCommandName && h == "" {
				h = a
				continue
			}
			return "", o, false, nil
		}
		f := newFastPathFlag(a)
		switch f.name {
		case "--event":
			v, n, e := fastValue(args, i, f)
			if e != nil {
				return "", o, true, e
			}
			o.event = v
			i = n
		case "--queue":
			if f.hasInlineValue {
				return "", o, false, nil
			}
			o.queue = true
		default:
			return "", o, false, nil
		}
	}
	if command == hookCommandName && h == "" {
		return "", o, true, errFastPathRequiresHarness
	}
	return h, o, true, nil
}

func fastValue(args []string, i int, f fastPathFlag) (string, int, error) {
	if f.hasInlineValue {
		return f.inlineValue, i, nil
	}
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("%w for %s", errFastPathMissingValue, f.name)
	}
	return args[i+1], i + 1, nil
}

func kickQueueDrainerForArgs(args []string) {
	for _, arg := range args {
		if arg == drainQueueCommandName || arg == queueStatusCommandName {
			return
		}
	}
	kickQueueDrainer(context.Background(), "")
}
