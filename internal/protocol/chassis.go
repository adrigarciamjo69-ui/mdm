package protocol

// Tipos de chasis que informa el agente.
const (
	ChassisLaptop  = "laptop"
	ChassisDesktop = "desktop"
	ChassisServer  = "server"
	ChassisUnknown = "unknown"
)

// ChassisFromSMBIOS traduce los códigos de tipo de chasis de SMBIOS (los de
// Win32_SystemEnclosure.ChassisTypes y /sys/class/dmi/id/chassis_type). Gana
// el primer código reconocido.
func ChassisFromSMBIOS(codes []int) string {
	for _, c := range codes {
		switch c {
		case 8, 9, 10, 14, 30, 31, 32:
			return ChassisLaptop
		case 3, 4, 5, 6, 7, 13, 15, 16, 24, 35, 36:
			return ChassisDesktop
		case 17, 23, 25, 28, 29:
			return ChassisServer
		}
	}
	return ChassisUnknown
}
