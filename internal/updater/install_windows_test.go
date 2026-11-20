package updater

import (
	"os"
	"testing"
	"unsafe"
)

func TestShellExecuteInfoNativeLayout(t *testing.T) {
	wantSize, wantProcessOffset := uintptr(112), uintptr(104)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		wantSize, wantProcessOffset = 60, 56
	}
	var info shellExecuteInfo
	if unsafe.Sizeof(info) != wantSize || unsafe.Offsetof(info.Process) != wantProcessOffset {
		t.Fatalf("SHELLEXECUTEINFOW size = %d, process offset = %d", unsafe.Sizeof(info), unsafe.Offsetof(info.Process))
	}
}

func TestElevatedHelperRejectsIncompleteArguments(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"installer.exe", elevatedInstallArg}
	handled, err := elevatedHelper()
	if !handled || err == nil {
		t.Fatal("invalid elevated invocation must not install or open the main window")
	}
}
