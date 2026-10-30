package testkit

import (
	"os"

	sys "golang.org/x/sys/windows"
)

const windows = true

func chmod(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	inherit := ""
	if info.IsDir() {
		inherit = "OICI"
	}
	user, err := sys.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	owner := "FRFX"
	if mode&0o200 != 0 {
		owner = "FA"
	}
	sddl := "D:P(A;" + inherit + ";" + owner + ";;;" + user.User.Sid.String() + ")"
	switch {
	case mode&0o022 != 0:
		sddl += "(A;" + inherit + ";FA;;;WD)"
	case mode&0o044 != 0:
		sddl += "(A;" + inherit + ";FRFX;;;WD)"
	}
	sd, err := sys.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return sys.SetNamedSecurityInfo(path, sys.SE_FILE_OBJECT,
		sys.DACL_SECURITY_INFORMATION|sys.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
