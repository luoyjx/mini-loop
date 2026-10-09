package dotenv

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// DiscoveryContext carries host observations explicitly. CallerFiles lists
// Source-equivalent frames from nearest to oldest; Go runtime source paths are
// not silently treated as the Python deployment's origin.
type DiscoveryContext struct {
	WorkingDirectory string
	CallerFiles      []string
	Interactive      bool
	Debugger         bool
	Frozen           bool
}

type DiscoveryOptions struct {
	Context  DiscoveryContext
	Filename *string // nil defaults to .env; an empty name remains empty.
	UseCWD   bool
	Required bool
}

// Walk returns logical absolute ancestors, preserving symlinks in the path.
func Walk(start string) ([]string, *Failure) {
	info, err := os.Stat(start)
	if err != nil {
		return []string{}, &Failure{Class: FilesystemFailure, Message: "Starting path not found"}
	}
	if info.Mode().IsRegular() {
		start = filepath.Dir(start)
	}
	current, err := filepath.Abs(start)
	if err != nil {
		return nil, fileFailure(err)
	}
	paths := []string{}
	for {
		paths = append(paths, current)
		parent := filepath.Dir(current)
		if parent == current {
			return paths, nil
		}
		current = parent
	}
}

func fileOrFIFO(path string) bool {
	info, err := os.Stat(path)
	return err == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeNamedPipe != 0)
}

// Find implements Source origin selection and upward search without changing
// cwd or opening the selected file. Missing caller evidence is refused.
func Find(options DiscoveryOptions) (string, *Failure) {
	context := options.Context
	cwd := context.WorkingDirectory
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fileFailure(err)
		}
	}
	if !filepath.IsAbs(cwd) {
		absolute, err := filepath.Abs(cwd)
		if err != nil {
			return "", fileFailure(err)
		}
		cwd = absolute
	}
	start := cwd
	if !options.UseCWD && !context.Interactive && !context.Debugger && !context.Frozen {
		start = ""
		for _, caller := range context.CallerFiles {
			if caller == "" {
				continue
			}
			if !filepath.IsAbs(caller) {
				caller = filepath.Join(cwd, caller)
			}
			if _, err := os.Stat(caller); err == nil {
				start = filepath.Dir(caller)
				break
			}
		}
		if start == "" {
			return "", &Failure{Class: OriginFailure, Message: "dotenv caller origin unavailable"}
		}
	}
	paths, fault := Walk(start)
	if fault != nil {
		return "", fault
	}
	filename := ".env"
	if options.Filename != nil {
		filename = *options.Filename
	}
	for _, path := range paths {
		prefix := path
		if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
			prefix += string(os.PathSeparator)
		}
		candidate := prefix + filename
		// os.path.join discards its prefix for an absolute second operand.
		if filepath.IsAbs(filename) {
			candidate = filename
		}
		if strings.ContainsRune(candidate, 0) {
			return "", &Failure{Class: InvalidEnvironment, Message: "embedded null byte"}
		}
		if fileOrFIFO(candidate) {
			return candidate, nil
		}
	}
	if options.Required {
		return "", &Failure{Class: FilesystemFailure, Message: "File not found"}
	}
	return "", nil
}

type Encoding string

const (
	UTF8          Encoding = "utf-8"
	UTF8Signature Encoding = "utf-8-sig"
	Latin1        Encoding = "latin-1"
)

// FileOptions selects file-first or fallback text, then optional discovery.
// Nil Path with nil Stream discovers; an explicit empty path suppresses it.
// Codecs are finite; the zero value means UTF-8. Literal disables interpolation.
type FileOptions struct {
	Path      *string
	Stream    *string
	Encoding  Encoding
	Literal   bool
	Discovery DiscoveryOptions
}

type FileValues struct {
	Values  []Variable
	Failure *Failure
}

func readText(options FileOptions) (string, *Failure) {
	path := ""
	if options.Path != nil {
		path = *options.Path
	} else if options.Stream == nil {
		var fault *Failure
		path, fault = Find(options.Discovery)
		if fault != nil {
			return "", fault
		}
	}
	if strings.ContainsRune(path, 0) {
		return "", &Failure{Class: InvalidEnvironment, Message: "embedded null byte"}
	}
	if path != "" && fileOrFIFO(path) {
		file, err := os.Open(path)
		if err != nil {
			return "", fileFailure(err)
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return "", fileFailure(readErr)
		}
		if closeErr != nil {
			return "", fileFailure(closeErr)
		}
		return decodeFile(data, options.Encoding)
	}
	if options.Stream != nil {
		return *options.Stream, nil
	}
	return "", nil
}

func decodeFile(data []byte, encoding Encoding) (string, *Failure) {
	if encoding == "" {
		encoding = UTF8
	}
	var text string
	switch encoding {
	case UTF8, UTF8Signature:
		if encoding == UTF8Signature && len(data) >= 3 && string(data[:3]) == "\xef\xbb\xbf" {
			data = data[3:]
		}
		for offset := 0; offset < len(data); {
			_, size, issue := pytext.DecodeRune(data[offset:])
			if issue != pytext.ValidUTF8 {
				message := fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", data[offset], offset, issue)
				if size > 1 {
					message = fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s", offset, offset+size-1, issue)
				}
				return "", &Failure{Class: DecodeFailure, Message: message}
			}
			offset += size
		}
		text = string(data)
	case Latin1:
		var out strings.Builder
		for _, value := range data {
			out.WriteRune(rune(value))
		}
		text = out.String()
	default:
		return "", &Failure{Class: EncodingFailure, Message: "unsupported dotenv encoding: " + string(encoding)}
	}
	// TextIOWrapper universal newlines apply only to files, not supplied StringIO.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n"), nil
}

func fileValues(options FileOptions, environment Environment, override bool) FileValues {
	text, fault := readText(options)
	if fault != nil {
		return FileValues{Failure: fault}
	}
	bindings, err := Parse(text)
	if err != nil {
		return FileValues{Failure: &Failure{Class: InvalidEnvironment, Message: err.Error()}}
	}
	if options.Literal {
		// Settle duplicate bindings directly, preserving literal dollar syntax.
		values := []Variable{}
		index := map[Name]int{}
		for _, binding := range bindings {
			if binding.Error || binding.Key == nil {
				continue
			}
			var value *string
			if binding.Value != nil {
				detached := *binding.Value
				value = &detached
			}
			if i, ok := index[*binding.Key]; ok {
				values[i].Value = value
			} else {
				index[*binding.Key] = len(values)
				values = append(values, Variable{Key: *binding.Key, Value: value})
			}
		}
		return FileValues{Values: values}
	}
	return FileValues{Values: Resolve(bindings, environment, override)}
}

// Values resolves with override=true, like dotenv_values, even when loading is
// disabled. Its returned pointers/maps never alias caller inputs.
func Values(options FileOptions, environment Environment) FileValues {
	return fileValues(options, environment, true)
}

// LoadFile returns detached effects; disabled loading precedes discovery and
// decoding. Decode faults write nothing; application faults preserve prior writes.
// Like Source, a FIFO waits for its producer and is read to EOF without a cap.
func LoadFile(options FileOptions, environment Environment, override bool) Application {
	if loadingDisabled(environment) {
		return Apply(nil, environment, override)
	}
	values := fileValues(options, environment, override)
	if values.Failure != nil {
		result := Apply(nil, environment, override)
		result.Loaded = nil
		result.Failure = values.Failure
		return result
	}
	return Apply(values.Values, environment, override)
}

func fileFailure(err error) *Failure {
	class := FilesystemFailure
	switch {
	case errors.Is(err, os.ErrPermission):
		class = PermissionFailure
	case errors.Is(err, os.ErrNotExist):
		class = MissingFileFailure
	case errors.Is(err, syscall.EISDIR):
		class = DirectoryFailure
	}
	message := err.Error()
	var pathError *os.PathError
	var errno syscall.Errno
	if errors.As(err, &pathError) && errors.As(pathError.Err, &errno) {
		description := errno.Error()
		if len(description) > 0 {
			description = strings.ToUpper(description[:1]) + description[1:]
		}
		message = fmt.Sprintf("[Errno %d] %s: %s", errno, description, pytext.Repr(pathError.Path))
	}
	return &Failure{Class: class, Message: message}
}
