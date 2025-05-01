//go:build darwin

package modules

import (
	"syscall"
)

// assignTimestampsOSSpecific assigns Atime, Mtime, and Ctime to the StatDetails
// for Darwin (macOS), using the Atimespec, Mtimespec, and Ctimespec fields from syscall.Stat_t.
func assignTimestampsOSSpecific(statDetails *StatDetails, sysStat *syscall.Stat_t) {
	statDetails.Atime = timespecToFloat(sysStat.Atimespec)
	statDetails.Mtime = timespecToFloat(sysStat.Mtimespec)
	statDetails.Ctime = timespecToFloat(sysStat.Ctimespec)
}
