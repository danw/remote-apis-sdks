// Package command defines common types to be used with command execution.
package command

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bazelbuild/remote-apis-sdks/go/digest"
	"github.com/pborman/uuid"
)

// InputType can be specified to narrow down the matching for a given input path.
type InputType int

const (
	// Any input type will match.
	UnspecifiedInputType InputType = iota

	// Only directories match.
	DirectoryInputType

	// Only files match.
	FileInputType
)

var inputTypes = [...]string{"UnspecifiedInputType", "DirectoryInputType", "FileInputType"}

func (s InputType) String() string {
	if UnspecifiedInputType <= s && s <= FileInputType {
		return inputTypes[s]
	}
	return fmt.Sprintf("InvalidInputType(%d)", s)
}

// InputExclusion represents inputs to be excluded from being considered for command execution.
type InputExclusion struct {
	// Required: the path regular expression to match for exclusion.
	Regex string

	// The input type to match for exclusion.
	Type InputType
}

// InputSpec represents all the required inputs to a remote command.
type InputSpec struct {
	// Input paths (files or directories) that need to be present for the command execution.
	Inputs []string

	// Inputs matching these patterns will be excluded.
	InputExclusions []*InputExclusion

	// Environment variables the command relies on.
	EnvironmentVariables map[string]string
}

type Identifiers struct {
	// An optional id to use to identify a command.
	CommandId string

	// An optional id to use to identify an invocation spanning multiple commands.
	InvocationId string

	// An optional id to use to identify a build spanning multiple invocations.
	CorrelatedInvocationId string

	// An optional tool name to pass to the remote server for logging.
	ToolName string

	// An optional tool version to pass to the remote server for logging.
	ToolVersion string

	// A UUID generated for a particular execution of this command.
	ExecutionId string
}

// Command encompasses the complete information required to execute a command remotely.
// To make sure to initialize a valid Command object, call FillDefaultFieldValues on the created
// struct.
type Command struct {
	// Identifiers used to identify this command to be passed to RE.
	Identifiers *Identifiers

	// Required: command line elements to execute.
	Args []string

	// An absolute path to the execution root of the command. All the other paths are
	// specified relatively to this path.
	ExecRoot string

	// The working directory, relative to the exec root, for the command to run
	// in. It must be a directory which exists in the input tree. If it is left
	// empty, then the action is run from the exec root.
	WorkingDir string

	// The command inputs.
	InputSpec *InputSpec

	// The command output files.
	OutputFiles []string

	// The command output directories.
	// The files and directories will likely be merged into a single Outputs field in the future.
	OutputDirs []string

	// Optional duration to wait for command execution before timing out.
	Timeout time.Duration

	// The platform to use for the execution.
	Platform map[string]string
}

func marshallMap(m map[string]string, buf *[]byte) {
	var pkeys []string
	for k := range m {
		pkeys = append(pkeys, k)
	}
	sort.Strings(pkeys)
	for _, k := range pkeys {
		*buf = append(*buf, []byte(k)...)
		*buf = append(*buf, []byte(m[k])...)
	}
}

func marshallSlice(s []string, buf *[]byte) {
	for _, i := range s {
		*buf = append(*buf, []byte(i)...)
	}
}

func marshallSortedSlice(s []string, buf *[]byte) {
	ss := make([]string, len(s))
	copy(ss, s)
	sort.Strings(ss)
	marshallSlice(ss, buf)
}

// Validate checks whether all required command fields have been specified.
func (c *Command) Validate() error {
	if c == nil {
		return nil
	}
	if c.Args == nil {
		return errors.New("missing command arguments")
	}
	if c.ExecRoot == "" {
		return errors.New("missing command exec root")
	}
	if c.InputSpec == nil {
		return errors.New("missing command input spec")
	}
	if c.Identifiers == nil {
		return errors.New("missing command identifiers")
	}
	return nil
}

// Generates a stable id for the command.
func (c *Command) stableId() string {
	var buf []byte
	marshallSlice(c.Args, &buf)
	buf = append(buf, []byte(c.ExecRoot)...)
	buf = append(buf, []byte(c.WorkingDir)...)
	marshallSortedSlice(c.OutputFiles, &buf)
	marshallSortedSlice(c.OutputDirs, &buf)
	buf = append(buf, []byte(c.Timeout.String())...)
	marshallMap(c.Platform, &buf)
	if c.InputSpec != nil {
		marshallMap(c.InputSpec.EnvironmentVariables, &buf)
		marshallSortedSlice(c.InputSpec.Inputs, &buf)
		inputExclusions := make([]*InputExclusion, len(c.InputSpec.InputExclusions))
		copy(inputExclusions, c.InputSpec.InputExclusions)
		sort.Slice(inputExclusions, func(i, j int) bool {
			e1 := inputExclusions[i]
			e2 := inputExclusions[j]
			return e1.Regex > e2.Regex || e1.Regex == e2.Regex && e1.Type > e2.Type
		})
		for _, e := range inputExclusions {
			buf = append(buf, []byte(e.Regex)...)
			buf = append(buf, []byte(e.Type.String())...)
		}
	}
	sha256Arr := sha256.Sum256(buf)
	return hex.EncodeToString(sha256Arr[:])[:8]
}

// FillDefaultFieldValues initializes valid default values to inner Command fields.
// This function should be called on every new Command object before use.
func (c *Command) FillDefaultFieldValues() {
	if c == nil {
		return
	}
	if c.Identifiers == nil {
		c.Identifiers = &Identifiers{}
	}
	if c.Identifiers.CommandId == "" {
		c.Identifiers.CommandId = c.stableId()
	}
	if c.Identifiers.ToolName == "" {
		c.Identifiers.ToolName = "remote-client"
	}
	if c.Identifiers.InvocationId == "" {
		c.Identifiers.InvocationId = uuid.New()
	}
	if c.InputSpec == nil {
		c.InputSpec = &InputSpec{}
	}
}

// ExecutionOptions specify how to execute a given Command.
type ExecutionOptions struct {
	// Whether to accept cached action results. Defaults to true.
	AcceptCached bool

	// When set, this execution results will not be cached.
	DoNotCache bool

	// Download command outputs after execution. Defaults to true.
	DownloadOutputs bool
}

// DefaultExecutionOptions returns the recommended ExecutionOptions.
func DefaultExecutionOptions() *ExecutionOptions {
	return &ExecutionOptions{
		AcceptCached:    true,
		DoNotCache:      false,
		DownloadOutputs: true,
	}
}

// ResultStatus represents the options for a finished command execution.
type ResultStatus int

const (
	// Command executed successfully.
	SuccessResultStatus ResultStatus = iota

	// Command was a cache hit.
	CacheHitResultStatus

	// Command exceeded its specified deadline.
	TimeoutResultStatus

	// The execution was interrupted.
	InterruptedResultStatus

	// An error occurred on the remote server.
	RemoteErrorResultStatus

	// An error occurred locally.
	LocalErrorResultStatus
)

var resultStatuses = [...]string{
	"SuccessResultStatus",
	"CacheHitResultStatus",
	"TimeoutResultStatus",
	"InterruptedResultStatus",
	"RemoteErrorResultStatus",
	"LocalErrorResultStatus",
}

func (s ResultStatus) String() string {
	if SuccessResultStatus <= s && s <= LocalErrorResultStatus {
		return resultStatuses[s]
	}
	return fmt.Sprintf("InvalidResultStatus(%d)", s)
}

// Result is the result of a finished command execution.
type Result struct {
	// Command exit code.
	ExitCode int
	// Status of the finished run.
	Status ResultStatus
	// Any error encountered.
	Err error
}

// Metadata is general information associated with a Command execution.
type Metadata struct {
	// CommandDigest is a digest of the command being executed. It can be used
	// to detect changes in the command between builds.
	CommandDigest digest.Digest
	// ActionDigest is a digest of the action being executed. It can be used
	// to detect changes in the action between builds.
	ActionDigest digest.Digest
	// TODO(olaola): Add a lot of other fields.
}