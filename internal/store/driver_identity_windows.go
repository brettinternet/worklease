package store

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processHandleInformation is PROCESSINFOCLASS ProcessHandleInformation.
const processHandleInformation = 51

type processHandleEntry struct {
	HandleValue      windows.Handle
	HandleCount      uintptr
	PointerCount     uintptr
	GrantedAccess    uint32
	ObjectTypeIndex  uint32
	HandleAttributes uint32
	Reserved         uint32
}

// OpenedDatabaseIdentities verifies the live SQLite handles, not merely
// another lookup of the database pathname.
func (d *Driver) OpenedDatabaseIdentities() ([]fileIdentity, error) {
	handles, err := processHandles()
	if err != nil {
		return nil, fmt.Errorf("enumerate open handles: %w", err)
	}
	file, err := os.Open(d.path)
	if err != nil {
		return nil, fmt.Errorf("resolve database handle path: %w", err)
	}
	wanted, err := finalPath(windows.Handle(file.Fd()))
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("resolve database handle path: %w", err)
	}
	var identities []fileIdentity
	for _, h := range handles {
		// GetFinalPathNameByHandle can block on pipes; inspect disk files only.
		if kind, err := windows.GetFileType(h); err != nil || kind != windows.FILE_TYPE_DISK {
			continue
		}
		if path, err := finalPath(h); err != nil || !strings.EqualFold(path, wanted) {
			continue
		}
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err != nil {
			continue
		}
		identities = append(identities, fileIdentity{Dev: uint64(info.VolumeSerialNumber), Ino: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)})
	}
	if len(identities) == 0 {
		return nil, errors.New("SQLite database handle identity unavailable")
	}
	return identities, nil
}

func processHandles() ([]windows.Handle, error) {
	size := uint32(64 << 10)
	for {
		buffer := make([]byte, size)
		err := windows.NtQueryInformationProcess(windows.CurrentProcess(), processHandleInformation, unsafe.Pointer(&buffer[0]), size, &size)
		if errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) {
			size += 16 << 10
			continue
		}
		if err != nil {
			return nil, err
		}
		count := *(*uintptr)(unsafe.Pointer(&buffer[0]))
		entries := unsafe.Slice((*processHandleEntry)(unsafe.Pointer(&buffer[2*unsafe.Sizeof(uintptr(0))])), count)
		handles := make([]windows.Handle, len(entries))
		for i, entry := range entries {
			handles[i] = entry.HandleValue
		}
		return handles, nil
	}
}

func finalPath(h windows.Handle) (string, error) {
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), 0) // FILE_NAME_NORMALIZED|VOLUME_NAME_DOS
	if err != nil {
		return "", err
	}
	if int(n) >= len(buffer) {
		return "", windows.ERROR_INSUFFICIENT_BUFFER
	}
	return windows.UTF16ToString(buffer[:n]), nil
}
