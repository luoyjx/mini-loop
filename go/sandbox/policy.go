package sandbox

import (
	"fmt"
	"strings"
)

const basePolicy = `(version 1)
(deny default)

; child processes inherit this policy
(allow process-exec)
(allow process-fork)
(allow signal (target same-sandbox))
(allow process-info* (target same-sandbox))

; a process that cannot write /dev/null cannot redirect output
(allow file-write-data
  (require-all
    (path "/dev/null")
    (vnode-type CHARACTER-DEVICE)))

; machine facts: without these, tooling that probes the CPU or hostname fails
(allow sysctl-read)
(allow mach-lookup)
(allow ipc-posix-sem)
(allow user-preference-read)

; interactive tools expect a tty
(allow pseudo-tty)
(allow file-read* file-write* file-ioctl (literal "/dev/ptmx"))
`
const networkPolicy = `
(allow network-outbound)
(allow network-inbound)
(allow system-socket)
`

func (sandbox *SeatbeltSandbox) Argv(command string) ([]string, error) {
	lines := []string{basePolicy}
	params := make([]string, 0, len(sandbox.config.WritableRoots)+len(sandbox.config.UnreadableRoots))
	reads := make([]string, 0, len(sandbox.config.UnreadableRoots))
	for index, root := range sandbox.config.UnreadableRoots {
		key := fmt.Sprintf("NOREAD_%d", index)
		params = append(params, key+"="+root)
		reads = append(reads, fmt.Sprintf(`(require-not (literal (param "%s"))) (require-not (subpath (param "%s")))`, key, key))
	}
	if len(reads) == 0 {
		lines = append(lines, "; reads allowed\n(allow file-read*)")
	} else {
		lines = append(lines, "; reads allowed except the protected roots\n(allow file-read* (require-all "+strings.Join(reads, " ")+" ))")
	}
	writes := make([]string, 0, len(sandbox.config.WritableRoots))
	for index, root := range sandbox.config.WritableRoots {
		key := fmt.Sprintf("WRITE_%d", index)
		params = append(params, key+"="+root)
		writes = append(writes, fmt.Sprintf(`(subpath (param "%s"))`, key))
	}
	lines = append(lines, "; writes confined to the declared roots\n(allow file-write* "+strings.Join(writes, " ")+")")
	if sandbox.config.AllowNetwork {
		lines = append(lines, networkPolicy)
	}
	argv := []string{Binary, "-p", strings.Join(lines, "\n")}
	for _, param := range params {
		argv = append(argv, "-D", param)
	}
	return append(argv, shellBinary, "-c", command), nil
}
