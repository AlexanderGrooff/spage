//go:build linux

package modules

import (
	"syscall"
)

// assignTimestampsOSSpecific assigns Atime, Mtime, and Ctime to the StatDetails
// for Linux, using the Atim, Mtim, and Ctim fields from syscall.Stat_t.
func assignTimestampsOSSpecific(statDetails *StatDetails, sysStat *syscall.Stat_t) {
	statDetails.Atime = timespecToFloat(sysStat.Atim)
	statDetails.Mtime = timespecToFloat(sysStat.Mtim)
	statDetails.Ctime = timespecToFloat(sysStat.Ctim)
}
