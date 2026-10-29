package procgroup

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Group is a started command's process tree.
type Group struct{ job windows.Handle }

// Prepare configures cmd, before Start, to lead a new group.
func Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

// Attach places a command started after Prepare in a new job object. A
// descendant the command starts before Attach returns is not a member.
func Attach(cmd *exec.Cmd) (*Group, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &Group{job: job}, nil
}

// Terminate asks every member to exit. It fails when the group is empty.
// Windows has no request signal and ends every member immediately.
func (g *Group) Terminate() error {
	if g.Empty() {
		return syscall.ESRCH
	}
	return g.Kill()
}

// Kill ends every member immediately.
func (g *Group) Kill() error { return windows.TerminateJobObject(g.job, 1) }

// Empty reports whether no member remains.
func (g *Group) Empty() bool {
	var info struct {
		TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
		TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
	}
	err := windows.QueryInformationJobObject(g.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	return err != nil || info.ActiveProcesses == 0
}

// Close releases the group's resources without signaling it.
func (g *Group) Close() error { return windows.CloseHandle(g.job) }
