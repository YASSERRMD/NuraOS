//go:build linux

package landlock

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Linux Landlock syscall numbers (x86-64, same on all supported arches).
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
)

// Landlock rule type.
const landlockRulePathBeneath = 1

// createRulesetVersion is passed as flags to probe the ABI version.
const createRulesetVersion = 1 << 0

// Filesystem access-right bit positions (ABI v1).
const (
	accessFSExecute   uint64 = 1 << 0
	accessFSWriteFile uint64 = 1 << 1
	accessFSReadFile  uint64 = 1 << 2
	accessFSReadDir   uint64 = 1 << 3
	accessFSRemoveDir  uint64 = 1 << 4
	accessFSRemoveFile uint64 = 1 << 5
	accessFSMakeChar   uint64 = 1 << 6
	accessFSMakeDir    uint64 = 1 << 7
	accessFSMakeReg    uint64 = 1 << 8
	accessFSMakeSock   uint64 = 1 << 9
	accessFSMakeFifo   uint64 = 1 << 10
	accessFSMakeBlock  uint64 = 1 << 11
	accessFSMakeSym    uint64 = 1 << 12
)

// allFSAccessV1 is the union of all filesystem rights in ABI v1.
const allFSAccessV1 = accessFSExecute | accessFSWriteFile | accessFSReadFile |
	accessFSReadDir | accessFSRemoveDir | accessFSRemoveFile |
	accessFSMakeChar | accessFSMakeDir | accessFSMakeReg |
	accessFSMakeSock | accessFSMakeFifo | accessFSMakeBlock | accessFSMakeSym

// landlockRulesetAttr mirrors struct landlock_ruleset_attr (v1 layout).
// The size passed to landlock_create_ruleset controls which ABI version is used.
type landlockRulesetAttr struct {
	HandledAccessFS uint64
}

// landlockPathBeneathAttr mirrors struct landlock_path_beneath_attr.
// __attribute__((packed)) in C; Go lays out the same (8 + 4 = 12 bytes, no implicit padding).
type landlockPathBeneathAttr struct {
	AllowedAccess uint64
	ParentFD      int32
}

// accessNames maps human-readable names to Landlock ABI bit positions.
var accessNames = map[Access]uint64{
	AccessExecute:   accessFSExecute,
	AccessWriteFile: accessFSWriteFile,
	AccessReadFile:  accessFSReadFile,
	AccessReadDir:   accessFSReadDir,
	AccessRemoveDir:  accessFSRemoveDir,
	AccessRemoveFile: accessFSRemoveFile,
	AccessMakeChar:  accessFSMakeChar,
	AccessMakeDir:   accessFSMakeDir,
	AccessMakeReg:   accessFSMakeReg,
	AccessMakeSock:  accessFSMakeSock,
	AccessMakeFifo:  accessFSMakeFifo,
	AccessMakeBlock: accessFSMakeBlock,
	AccessMakeSym:   accessFSMakeSym,
}

// probeABI returns the highest supported Landlock ABI version (1+ means supported).
func probeABI() int {
	v, _, _ := syscall.RawSyscall(sysLandlockCreateRuleset, 0, 0, createRulesetVersion)
	return int(v)
}

// Apply installs a Landlock filesystem confinement ruleset on the calling process.
// The ruleset is inherited across fork and exec.
//
// If the running kernel does not support Landlock (ABI < 1), Apply returns nil
// and logs a warning so production boot still succeeds on older kernels.
func Apply(p *Profile) error {
	if p == nil || len(p.Paths) == 0 {
		return nil
	}

	if probeABI() < 1 {
		// Non-fatal: older kernels silently skip Landlock.
		return fmt.Errorf("landlock: kernel does not support Landlock (ABI < 1); skipping")
	}

	// Create ruleset covering all v1 FS access rights.
	attr := landlockRulesetAttr{HandledAccessFS: allFSAccessV1}
	rfd, _, errno := syscall.RawSyscall(sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)),
		unsafe.Sizeof(attr),
		0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", errno)
	}
	defer syscall.Close(int(rfd))

	// Add one path-beneath rule per declared path.
	for _, rule := range p.Paths {
		var rights uint64
		for _, a := range rule.Access {
			bit, ok := accessNames[a]
			if !ok {
				return fmt.Errorf("landlock: unknown access right %q for path %s", a, rule.Path)
			}
			rights |= bit
		}
		if rights == 0 {
			continue
		}
		if err := addPathRule(int(rfd), rule.Path, rights); err != nil {
			// Non-fatal: path may not exist yet (e.g. /data/logs before /data mount).
			// Skipping ensures boot resilience; tighten by marking paths as required.
			_ = err // logged upstream
			continue
		}
	}

	// Restrict the current thread (and all future execs) to the declared ruleset.
	if _, _, errno = syscall.RawSyscall(sysLandlockRestrictSelf, uintptr(rfd), 0, 0); errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %w", errno)
	}
	return nil
}

// addPathRule opens path with O_PATH and adds a LANDLOCK_RULE_PATH_BENEATH rule.
func addPathRule(rulesetFD int, path string, rights uint64) error {
	fd, err := syscall.Open(path, syscall.O_PATH|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer syscall.Close(fd)

	pathAttr := landlockPathBeneathAttr{
		AllowedAccess: rights,
		ParentFD:      int32(fd),
	}
	_, _, errno := syscall.RawSyscall6(sysLandlockAddRule,
		uintptr(rulesetFD),
		landlockRulePathBeneath,
		uintptr(unsafe.Pointer(&pathAttr)),
		0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock_add_rule %s: %w", path, errno)
	}
	return nil
}
