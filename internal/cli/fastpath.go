package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/zigai/agent-sessions/pkg/registry"
)

type fastPathGlobals struct {
	storePath  string
	outputJSON bool
}

type fastPathFlag struct {
	name           string
	inlineValue    string
	hasInlineValue bool
}

type fastReportParseState struct {
	options            reportOptions
	positionals        []string
	cwdChanged         bool
	projectRootChanged bool
}

type (
	fastReportStringSetter func(*fastReportParseState, string)
	fastReportIntSetter    func(*fastReportParseState, int)
	fastReportSliceSetter  func(*fastReportParseState, string)
	fastReportBoolSetter   func(*fastReportParseState)
)

const (
	agyHookCommandName = "agy-hook"
	fastPathEventFlag  = "--event"
)

var (
	errFastPathMissingValue      = errors.New("missing value")
	errFastPathTooManyReportArgs = errors.New("too many report arguments")
	errFastPathRequiresHarness   = errors.New("requires a harness")
)

var fastReportStringFlags = map[string]fastReportStringSetter{
	"--harness": func(state *fastReportParseState, value string) {
		state.options.harness = value
	},
	"--state": func(state *fastReportParseState, value string) {
		state.options.state = value
	},
	"--session-id": func(state *fastReportParseState, value string) {
		state.options.sessionID = value
	},
	"--session-path": func(state *fastReportParseState, value string) {
		state.options.sessionPath = value
	},
	"--cwd": func(state *fastReportParseState, value string) {
		state.options.cwd = value
		state.cwdChanged = true
	},
	"--project-root": func(state *fastReportParseState, value string) {
		state.options.projectRoot = value
		state.projectRootChanged = true
	},
	"--tty": func(state *fastReportParseState, value string) {
		state.options.tty = value
	},
	"--source": func(state *fastReportParseState, value string) {
		state.options.source = value
	},
	"--confidence": func(state *fastReportParseState, value string) {
		state.options.confidence = value
	},
	fastPathEventFlag: func(state *fastReportParseState, value string) {
		state.options.event = value
	},
	"--observed-at": func(state *fastReportParseState, value string) {
		state.options.observedAt = value
	},
}

var fastReportIntFlags = map[string]fastReportIntSetter{
	"--pid": func(state *fastReportParseState, value int) {
		state.options.pid = value
	},
	"--ppid": func(state *fastReportParseState, value int) {
		state.options.ppid = value
	},
}

var fastReportSliceFlags = map[string]fastReportSliceSetter{
	"--attribute": func(state *fastReportParseState, value string) {
		state.options.attributes = append(state.options.attributes, value)
	},
	"--resume-command": func(state *fastReportParseState, value string) {
		state.options.resumeCommand = append(state.options.resumeCommand, value)
	},
}

var fastReportBoolFlags = map[string]fastReportBoolSetter{
	"--raw-stdin": func(state *fastReportParseState) {
		state.options.rawStdin = true
	},
	"--raw-stdin-defaults-only": func(state *fastReportParseState) {
		state.options.rawDefaultsOnly = true
	},
	"--no-tmux": func(state *fastReportParseState) {
		state.options.noTmux = true
	},
	"--quiet": func(state *fastReportParseState) {
		state.options.quiet = true
	},
}

func tryExecuteFastPath(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (bool, error) {
	globals, command, commandArgs, ok := splitFastPathArgs(args)
	if !ok {
		return false, nil
	}

	app := &application{
		storePath:  globals.storePath,
		outputJSON: globals.outputJSON,
		stdout:     stdout,
		stderr:     stderr,
	}

	switch command {
	case reportCommandName:
		return executeFastReport(ctx, app, stdin, commandArgs)
	case hookCommandName, agyHookCommandName:
		return executeFastManagedHook(ctx, app, stdin, command, commandArgs)
	default:
		return false, nil
	}
}

func splitFastPathArgs(args []string) (fastPathGlobals, string, []string, bool) {
	var globals fastPathGlobals
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--store":
			if index+1 >= len(args) {
				return globals, "", nil, false
			}
			index++
			globals.storePath = args[index]
		case strings.HasPrefix(arg, "--store="):
			globals.storePath = strings.TrimPrefix(arg, "--store=")
		case arg == "--json":
			globals.outputJSON = true
		case strings.HasPrefix(arg, "-"):
			return globals, "", nil, false
		case arg == reportCommandName || arg == hookCommandName || arg == agyHookCommandName:
			return globals, arg, args[index+1:], true
		default:
			return globals, "", nil, false
		}
	}

	return globals, "", nil, false
}

func executeFastReport(ctx context.Context, app *application, stdin io.Reader, args []string) (bool, error) {
	options, ok, err := parseFastReportOptions(args)
	if !ok || err != nil {
		return ok, err
	}

	return true, app.runReport(ctx, stdin, options)
}

func parseFastReportOptions(args []string) (reportOptions, bool, error) {
	state := fastReportParseState{
		options: defaultReportOptionsFromEnv(),
	}

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--":
			state.positionals = append(state.positionals, args[index+1:]...)
			index = len(args)
		case !strings.HasPrefix(arg, "--"):
			state.positionals = append(state.positionals, arg)
		default:
			nextIndex, ok, err := applyFastReportFlag(args, index, &state)
			if err != nil || !ok {
				return state.options, ok, err
			}
			index = nextIndex
		}
	}

	options, err := finishFastReportOptions(&state)
	return options, true, err
}

func newFastPathFlag(arg string) fastPathFlag {
	name, inlineValue, hasInlineValue := strings.Cut(arg, "=")

	return fastPathFlag{
		name:           name,
		inlineValue:    inlineValue,
		hasInlineValue: hasInlineValue,
	}
}

func applyFastReportFlag(args []string, index int, state *fastReportParseState) (int, bool, error) {
	flag := newFastPathFlag(args[index])
	if setter, ok := fastReportStringFlags[flag.name]; ok {
		return applyFastReportStringFlag(args, index, flag, state, setter)
	}
	if setter, ok := fastReportIntFlags[flag.name]; ok {
		return applyFastReportIntFlag(args, index, flag, state, setter)
	}
	if setter, ok := fastReportSliceFlags[flag.name]; ok {
		return applyFastReportSliceFlag(args, index, flag, state, setter)
	}
	if setter, ok := fastReportBoolFlags[flag.name]; ok {
		if flag.hasInlineValue {
			return index, false, nil
		}
		setter(state)

		return index, true, nil
	}

	return index, false, nil
}

func applyFastReportStringFlag(
	args []string,
	index int,
	flag fastPathFlag,
	state *fastReportParseState,
	setter fastReportStringSetter,
) (int, bool, error) {
	value, nextIndex, err := fastPathFlagValue(args, index, flag)
	if err != nil {
		return index, true, err
	}
	setter(state, value)

	return nextIndex, true, nil
}

func applyFastReportIntFlag(
	args []string,
	index int,
	flag fastPathFlag,
	state *fastReportParseState,
	setter fastReportIntSetter,
) (int, bool, error) {
	value, nextIndex, err := fastPathFlagIntValue(args, index, flag)
	if err != nil {
		return index, true, err
	}
	setter(state, value)

	return nextIndex, true, nil
}

func applyFastReportSliceFlag(
	args []string,
	index int,
	flag fastPathFlag,
	state *fastReportParseState,
	setter fastReportSliceSetter,
) (int, bool, error) {
	value, nextIndex, err := fastPathFlagValue(args, index, flag)
	if err != nil {
		return index, true, err
	}
	setter(state, value)

	return nextIndex, true, nil
}

func fastPathFlagValue(args []string, index int, flag fastPathFlag) (string, int, error) {
	if flag.hasInlineValue {
		return flag.inlineValue, index, nil
	}
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%w for %s", errFastPathMissingValue, flag.name)
	}

	return args[index+1], index + 1, nil
}

func fastPathFlagIntValue(args []string, index int, flag fastPathFlag) (int, int, error) {
	value, nextIndex, err := fastPathFlagValue(args, index, flag)
	if err != nil {
		return 0, index, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, index, fmt.Errorf("parsing %s: %w", flag.name, err)
	}

	return parsed, nextIndex, nil
}

func finishFastReportOptions(state *fastReportParseState) (reportOptions, error) {
	if len(state.positionals) > maxReportArgs {
		return state.options, fmt.Errorf(
			"accepts at most %d arg(s), received %d: %w",
			maxReportArgs,
			len(state.positionals),
			errFastPathTooManyReportArgs,
		)
	}
	if err := applyReportArgs(state.positionals, &state.options); err != nil {
		return state.options, err
	}
	if state.cwdChanged {
		state.options.cwdAuto = false
	}
	if state.projectRootChanged {
		state.options.projectRootAuto = false
	}

	return state.options, nil
}

func executeFastManagedHook(
	ctx context.Context,
	app *application,
	stdin io.Reader,
	command string,
	args []string,
) (bool, error) {
	harnessName, options, ok, err := parseFastManagedHookOptions(command, args)
	if !ok || err != nil {
		return ok, err
	}

	return true, app.runManagedHook(ctx, stdin, harnessName, options)
}

func parseFastManagedHookOptions(command string, args []string) (string, managedHookOptions, bool, error) {
	var options managedHookOptions
	harnessName := defaultFastManagedHookHarness(command)

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--":
			if trailingHarness, ok := fastManagedHookTerminatorHarness(command, harnessName, args[index+1:]); ok {
				harnessName = trailingHarness
				index = len(args)

				continue
			}
			return "", options, false, nil
		case !strings.HasPrefix(arg, "--"):
			if nextHarness, ok := fastManagedHookPositionalHarness(command, harnessName, arg); ok {
				harnessName = nextHarness
				continue
			}
			return "", options, false, nil
		default:
			nextIndex, ok, err := applyFastManagedHookFlag(args, index, &options)
			if err != nil || !ok {
				return "", options, ok, err
			}
			index = nextIndex
		}
	}
	if command == hookCommandName && harnessName == "" {
		return "", options, true, errFastPathRequiresHarness
	}

	return harnessName, options, true, nil
}

func defaultFastManagedHookHarness(command string) string {
	if command == agyHookCommandName {
		return string(registry.HarnessAgy)
	}

	return ""
}

func fastManagedHookTerminatorHarness(command string, harnessName string, trailing []string) (string, bool) {
	if command == hookCommandName && harnessName == "" && len(trailing) == 1 {
		return trailing[0], true
	}

	return "", false
}

func fastManagedHookPositionalHarness(command string, harnessName string, arg string) (string, bool) {
	if command == hookCommandName && harnessName == "" {
		return arg, true
	}

	return "", false
}

func applyFastManagedHookFlag(args []string, index int, options *managedHookOptions) (int, bool, error) {
	flag := newFastPathFlag(args[index])
	if flag.name != fastPathEventFlag {
		return index, false, nil
	}
	value, nextIndex, err := fastPathFlagValue(args, index, flag)
	if err != nil {
		return index, true, err
	}
	options.event = value

	return nextIndex, true, nil
}
