//go:build windows

package agentsession

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// attachJob puts the child in a kill-on-close job object, so terminating
// the job (or gg dying and the handle closing) takes the whole tree. The
// child can spawn before assignment; ConPTY children normally start their
// own subprocesses later, which the job then catches.
func attachJob(s *Session) {
	if s.cmd.Process == nil {
		return
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	_, _ = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(s.cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(h)
	if windows.AssignProcessToJobObject(job, h) != nil {
		_ = windows.CloseHandle(job)
		return
	}
	s.job = uintptr(job)
}

// kill terminates the job (the whole tree) or, without one, the process.
func (s *Session) kill() {
	if !s.running() {
		return
	}
	if s.job != 0 {
		_ = windows.TerminateJobObject(windows.Handle(s.job), 1)
	} else if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}
