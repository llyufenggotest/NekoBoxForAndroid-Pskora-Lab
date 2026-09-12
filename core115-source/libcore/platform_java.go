package libcore

import "libcore/androidmonitor"

type InterfaceUpdateListener interface {
	NetworkMonitorID() int64
	UpdateDefaultInterface(name string, index int32, expensive bool, constrained bool)
}

type javaNetworkPlatform struct{}

func (javaNetworkPlatform) StartDefaultInterfaceMonitor(l androidmonitor.Listener) error {
	return intfBox.StartDefaultInterfaceMonitor(l)
}
func (javaNetworkPlatform) CloseDefaultInterfaceMonitor(l androidmonitor.Listener) error {
	return intfBox.CloseDefaultInterfaceMonitor(l)
}

var intfBox BoxPlatformInterface
var intfNB4A NB4AInterface

var useProcfs bool
var isBgProcess bool

type NB4AInterface interface {
	UseOfficialAssets() bool
	Selector_OnProxySelected(selectorTag string, tag string)
}

type BoxPlatformInterface interface {
	StartDefaultInterfaceMonitor(listener InterfaceUpdateListener) error
	CloseDefaultInterfaceMonitor(listener InterfaceUpdateListener) error
	NetworkInterfacesJSON() (string, error)
	AutoDetectInterfaceControl(fd int32) error
	OpenTun(singTunOptionsJson, tunPlatformOptionsJson string) (int, error)
	UseProcFS() bool
	FindConnectionOwner(ipProtocol int32, sourceAddress string, sourcePort int32, destinationAddress string, destinationPort int32) (int32, error)
	PackageNameByUid(uid int32) (string, error)
	UIDByPackageName(packageName string) (int32, error)
	WIFIState() string
}
