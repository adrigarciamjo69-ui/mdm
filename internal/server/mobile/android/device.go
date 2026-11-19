package android

import "time"

// Device es la parte del recurso Device de la API que usa el MDM.
type Device struct {
	Name                 string    `json:"name"`
	State                string    `json:"state"`
	PolicyName           string    `json:"policyName"`
	AppliedPolicyName    string    `json:"appliedPolicyName"`
	EnrollmentTime       time.Time `json:"enrollmentTime"`
	LastStatusReportTime time.Time `json:"lastStatusReportTime"`
	EnrollmentTokenData  string    `json:"enrollmentTokenData"`
	HardwareInfo         struct {
		Brand        string `json:"brand"`
		Manufacturer string `json:"manufacturer"`
		Model        string `json:"model"`
		SerialNumber string `json:"serialNumber"`
		Hardware     string `json:"hardware"`
	} `json:"hardwareInfo"`
	SoftwareInfo struct {
		AndroidVersion     string `json:"androidVersion"`
		SecurityPatchLevel string `json:"securityPatchLevel"`
		AndroidBuildNumber string `json:"androidBuildNumber"`
	} `json:"softwareInfo"`
	MemoryInfo struct {
		TotalRAM             string `json:"totalRam"`
		TotalInternalStorage string `json:"totalInternalStorage"`
	} `json:"memoryInfo"`
	NetworkInfo struct {
		IMEI           string `json:"imei"`
		MEID           string `json:"meid"`
		WifiMacAddress string `json:"wifiMacAddress"`
		TelephonyInfos []struct {
			CarrierName string `json:"carrierName"`
			PhoneNumber string `json:"phoneNumber"`
		} `json:"telephonyInfos"`
	} `json:"networkInfo"`
	SecurityPosture struct {
		DevicePosture string `json:"devicePosture"`
	} `json:"securityPosture"`
	DeviceSettings struct {
		IsEncrypted                bool `json:"isEncrypted"`
		IsDeviceSecure             bool `json:"isDeviceSecure"`
		DevelopmentSettingsEnabled bool `json:"developmentSettingsEnabled"`
		UnknownSourcesEnabled      bool `json:"unknownSourcesEnabled"`
	} `json:"deviceSettings"`
	ApplicationReports []struct {
		PackageName string `json:"packageName"`
		DisplayName string `json:"displayName"`
		VersionName string `json:"versionName"`
		State       string `json:"state"`
	} `json:"applicationReports"`
	NonComplianceDetails []struct {
		SettingName         string `json:"settingName"`
		NonComplianceReason string `json:"nonComplianceReason"`
		PackageName         string `json:"packageName"`
		FieldPath           string `json:"fieldPath"`
	} `json:"nonComplianceDetails"`
}
