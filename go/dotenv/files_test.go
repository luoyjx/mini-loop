package dotenv

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fileCorpus struct {
	Version   string
	Discovery []struct {
		Name, CWD, Caller, Filename, Kind, Selected string
		UseCWD, Interactive, Debugger, Frozen       bool
		Loaded                                      *bool
		Applied                                     Environment
	}
	Missing []struct {
		Required bool
		Selected *string
		Fault    *Failure
	}
	Walk []struct {
		Name  string
		Paths []string
		Fault *Failure
	}
	Reads []struct {
		Name, Kind, Content string
		Stream              *string
		Encoding            Encoding
		Environment         Environment
		Override            bool
		Literal             bool
		Values              []Variable
		ValueFault          *Failure
		Application
	}
}

func pinnedFileCorpus(t *testing.T) fileCorpus {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-dotenv-files.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus fileCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Version != SourceVersion || len(corpus.Discovery) != 10 || len(corpus.Reads) != 40 {
		t.Fatal("Source file corpus changed")
	}
	return corpus
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedSourceDiscoveryAndDefaultFileLoading(t *testing.T) {
	for _, row := range pinnedFileCorpus(t).Discovery {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			caller := filepath.Join(root, row.Caller)
			writeFixture(t, caller, nil)
			cwd := filepath.Join(root, row.CWD)
			if err := os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, filepath.Join(root, ".env"), []byte("ROOT=yes\n"))
			writeFixture(t, filepath.Join(root, "working/.env"), []byte("WORK=yes\n"))
			writeFixture(t, filepath.Join(root, "source/.env"), []byte("SOURCE=yes\n"))
			local := filepath.Join(filepath.Dir(caller), ".env")
			var err error
			switch row.Kind {
			case "parent":
			case "file":
				writeFixture(t, local, []byte("LOCAL=yes\n"))
			case "directory":
				err = os.Mkdir(local, 0700)
			case "symlink":
				err = os.Symlink("../.env", local)
			case "dangling":
				err = os.Symlink("absent", local)
			case "fifo":
				err = syscall.Mkfifo(local, 0600)
			default:
				t.Fatalf("unknown fixture kind %q", row.Kind)
			}
			if err != nil {
				t.Fatal(err)
			}
			options := DiscoveryOptions{Context: DiscoveryContext{WorkingDirectory: cwd,
				CallerFiles: []string{"", "absent-frame", caller}, Interactive: row.Interactive,
				Debugger: row.Debugger, Frozen: row.Frozen}, Filename: &row.Filename, UseCWD: row.UseCWD}
			selected, fault := Find(options)
			if fault != nil {
				t.Fatal(fault)
			}
			relative, err := filepath.Rel(root, selected)
			if err != nil || filepath.ToSlash(relative) != row.Selected {
				t.Fatal(selected, row.Selected, err)
			}
			if row.Loaded != nil {
				options.UseCWD = false // Source default load does not reuse explicit find(usecwd=True).
				result := LoadFile(FileOptions{Discovery: options}, Environment{}, true)
				if result.Failure != nil || !reflect.DeepEqual(result.Loaded, row.Loaded) || !reflect.DeepEqual(result.Environment, row.Applied) {
					t.Fatalf("default load: %#v; want %#v", result, row.Applied)
				}
			}
		})
	}
}

func TestPinnedSourceMissingFilesAndWalk(t *testing.T) {
	corpus := pinnedFileCorpus(t)
	root := t.TempDir()
	filename := ".missing-" + filepath.Base(root)
	for _, row := range corpus.Missing {
		selected, fault := Find(DiscoveryOptions{Context: DiscoveryContext{WorkingDirectory: root},
			Filename: &filename, UseCWD: true, Required: row.Required})
		if !reflect.DeepEqual(fault, row.Fault) {
			t.Fatal(fault, row.Fault)
		}
		if row.Selected != nil && selected != *row.Selected {
			t.Fatal(selected)
		}
	}
	caller := filepath.Join(root, "caller-parent/source/pkg/config.py")
	writeFixture(t, caller, nil)
	for _, row := range corpus.Walk {
		start := filepath.Dir(caller)
		if row.Name == "file" {
			start = caller
		}
		if row.Name == "missing" {
			start = filepath.Join(root, "absent")
		}
		paths, fault := Walk(start)
		if !reflect.DeepEqual(fault, row.Fault) {
			t.Fatal(fault, row.Fault)
		}
		prefix := []string{}
		for _, path := range paths {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				break
			}
			prefix = append(prefix, filepath.ToSlash(relative))
		}
		if !reflect.DeepEqual(prefix, row.Paths) {
			t.Fatal(prefix, row.Paths)
		}
	}
}

func TestPinnedSourceFileDecodingAndEffects(t *testing.T) {
	processBefore := os.Environ()
	for _, row := range pinnedFileCorpus(t).Reads {
		t.Run(row.Name+map[bool]string{true: "-override", false: "-preserve"}[row.Override], func(t *testing.T) {
			before, _ := json.Marshal(row)
			root := t.TempDir()
			path := filepath.Join(root, ".env")
			data, err := hex.DecodeString(row.Content)
			if err != nil {
				t.Fatal(err)
			}
			switch row.Kind {
			case "file":
				writeFixture(t, path, data)
			case "missing":
			case "nul-path":
				path = filepath.Join(root, "bad\x00name")
			case "directory":
				err = os.Mkdir(path, 0700)
			case "symlink":
				writeFixture(t, filepath.Join(root, "target"), data)
				err = os.Symlink("target", path)
			case "dangling":
				err = os.Symlink("absent", path)
			default:
				t.Fatalf("unknown fixture kind %q", row.Kind)
			}
			if err != nil {
				t.Fatal(err)
			}
			options := FileOptions{Path: &path, Stream: row.Stream, Encoding: row.Encoding, Literal: row.Literal}
			values := Values(options, row.Environment)
			if !reflect.DeepEqual(values.Values, row.Values) || !reflect.DeepEqual(values.Failure, row.ValueFault) {
				t.Fatalf("values: %#v; want %#v, %#v", values, row.Values, row.ValueFault)
			}
			application := LoadFile(options, row.Environment, row.Override)
			if !reflect.DeepEqual(application, row.Application) {
				t.Fatalf("application: %#v; want %#v", application, row.Application)
			}
			after, _ := json.Marshal(row)
			if string(before) != string(after) {
				t.Fatal("input changed")
			}
		})
	}
	if !reflect.DeepEqual(processBefore, os.Environ()) {
		t.Fatal("process environment changed")
	}
}

func TestFIFOReadToProducerEOF(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			_, err = file.WriteString("A='one\r\ntwo'\nB=${A}\n")
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		wrote <- err
	}()
	loaded := make(chan Application, 1)
	go func() { loaded <- LoadFile(FileOptions{Path: &path}, nil, true) }()
	select {
	case result := <-loaded:
		if result.Failure != nil || result.Loaded == nil || !*result.Loaded || result.Environment["B"] != "one\ntwo" {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO reader did not settle after producer EOF")
	}
	select {
	case err := <-wrote:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO producer did not close")
	}
}

func TestFileBoundariesAndLiteralSelection(t *testing.T) {
	root := t.TempDir()
	_, fault := Find(DiscoveryOptions{Context: DiscoveryContext{WorkingDirectory: root}})
	if fault == nil || fault.Class != OriginFailure {
		t.Fatal("missing caller evidence accepted")
	}
	stream := "A=${EXT}\nA=${NEXT}\nB\n"
	values := Values(FileOptions{Stream: &stream, Literal: true}, Environment{"NEXT": "expanded"})
	if values.Failure != nil || len(values.Values) != 2 || *values.Values[0].Value != "${NEXT}" || values.Values[1].Value != nil {
		t.Fatal(values)
	}
	path := filepath.Join(root, "input")
	writeFixture(t, path, []byte("A=one"))
	values = Values(FileOptions{Path: &path, Encoding: "unknown"}, nil)
	if values.Failure == nil || values.Failure.Class != EncodingFailure {
		t.Fatal(values)
	}
	result := LoadFile(FileOptions{}, Environment{"PYTHON_DOTENV_DISABLED": "TRUE"}, true)
	if result.Failure != nil || result.Loaded == nil || *result.Loaded {
		t.Fatal("disabled load tried discovery", result)
	}
	empty := ""
	result = LoadFile(FileOptions{Path: &empty}, nil, true)
	if result.Failure != nil || result.Loaded == nil || *result.Loaded {
		t.Fatal("explicit empty path tried discovery", result)
	}
	absolute, fault := Find(DiscoveryOptions{Context: DiscoveryContext{WorkingDirectory: root}, Filename: &path, UseCWD: true})
	if fault != nil || absolute != path {
		t.Fatal(absolute, fault)
	}
}
