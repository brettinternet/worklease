package runs

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// passReady lets the supervisor inherit only the readiness pipe, named by its
// handle value, and detaches it from the caller's console.
func passReady(cmd *exec.Cmd, writer *os.File) (string, error) {
	handle := windows.Handle(writer.Fd())
	if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return "", err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:              windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(handle)},
	}
	return strconv.FormatUint(uint64(handle), 10), nil
}

func openReady(value string) *os.File {
	handle, err := strconv.ParseUint(value, 10, 64)
	if err != nil || handle == 0 {
		return nil
	}
	if windows.SetHandleInformation(windows.Handle(handle), windows.HANDLE_FLAG_INHERIT, 0) != nil {
		return nil
	}
	return os.NewFile(uintptr(handle), "worklease-run-ready")
}

// stillActive is STATUS_PENDING, the exit code of a running process.
const stillActive = 259

func alive(pid int) bool {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(process)
	var code uint32
	return windows.GetExitCodeProcess(process, &code) == nil && code == stillActive
}
