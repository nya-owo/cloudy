package app

import (
	"sync"

	"golang.org/x/sys/windows"
)

// watchFolder calls changed, on a goroutine of its own, each time something
// in dir or the folders below it is added, removed, renamed or written.
// It returns what stops it, or nil when dir cannot be watched.
func watchFolder(dir string, changed func()) (stop func()) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil
	}
	h, err := windows.CreateFile(path, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil
	}
	got, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		windows.CloseHandle(h)
		return nil
	}
	quit, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(got)
		windows.CloseHandle(h)
		return nil
	}
	const mask = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
		windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE
	go func() {
		defer func() {
			windows.CloseHandle(got)
			windows.CloseHandle(quit)
			windows.CloseHandle(h)
		}()
		// What changed is not read: the folders are read again.
		buf := make([]byte, 16<<10)
		for {
			ov := windows.Overlapped{HEvent: got}
			if err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, mask, nil, &ov, 0); err != nil {
				return
			}
			ev, err := windows.WaitForMultipleObjects([]windows.Handle{got, quit}, false, windows.INFINITE)
			if err != nil || ev != windows.WAIT_OBJECT_0 {
				// Stopped: the read is cancelled, and waited for, before
				// its buffer and handle go.
				windows.CancelIoEx(h, &ov)
				var n uint32
				windows.GetOverlappedResult(h, &ov, &n, true)
				return
			}
			changed()
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { windows.SetEvent(quit) }) }
}
