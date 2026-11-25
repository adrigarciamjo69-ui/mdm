// Package policy define el catálogo de ajustes de las políticas y calcula la
// política efectiva de un equipo. Es lógica pura: no accede a la base de datos.
package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"mdm/internal/protocol"
)

// Tipos de parámetro.
const (
	TypeInt        = "int"
	TypeBool       = "bool"
	TypeString     = "string"
	TypeText       = "text"
	TypeEnum       = "enum"
	TypeStringList = "string_list"
	// TypeTime es una hora HH:MM.
	TypeTime = "time"
	// TypePackage es un nombre de paquete de Android (com.empresa.app).
	TypePackage = "package"
	// TypeSecret se guarda cifrado en el servidor y la consola no lo recibe de vuelta.
	TypeSecret = "secret"
)

type Field struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Default  any      `json:"default,omitempty"`
	Min      int      `json:"min,omitempty"`
	Max      int      `json:"max,omitempty"`
	Options  []string `json:"options,omitempty"`
	// OptionLabels son los textos de las opciones de un enum en la consola.
	OptionLabels map[string]string `json:"option_labels,omitempty"`
	MaxLen       int               `json:"max_len,omitempty"`
	Description  string            `json:"description,omitempty"`
}

type Def struct {
	Key         string `json:"key"`
	Platform    string `json:"platform"`
	Title       string `json:"title"`
	Description string `json:"description"`
	AuditOnly   bool   `json:"audit_only"`
	// EnforceOnly: el ajuste no se puede auditar (Android lo aplica él mismo).
	EnforceOnly bool    `json:"enforce_only"`
	Fields      []Field `json:"fields"`
	// check valida combinaciones de parámetros (opcional).
	check func(p map[string]any) error
}

const maxScriptBytes = 256 * 1024

func customScript(platform string, interpreters []string) Def {
	return Def{Key: protocol.CustomScriptKey, Platform: platform, Title: "Script personalizado",
		Description: "Un script de detección decide si el equipo cumple (código de salida 0). En modo «Aplicar», si no cumple se ejecuta el script de corrección y se vuelve a detectar.",
		Fields: []Field{
			{Name: "interpreter", Label: "Intérprete", Type: TypeEnum, Required: true, Options: interpreters},
			{Name: "detect", Label: "Script de detección", Type: TypeText, Required: true, MaxLen: maxScriptBytes},
			{Name: "remediate", Label: "Script de corrección", Type: TypeText, MaxLen: maxScriptBytes},
			{Name: "timeout_s", Label: "Tiempo máximo (segundos)", Type: TypeInt, Default: 300, Min: 1, Max: 3600},
		}}
}

// Catalog contiene todos los ajustes disponibles.
var Catalog = []Def{
	{Key: "windows.bitlocker", Platform: "windows", Title: "BitLocker obligatorio",
		Description: "Cifra la unidad del sistema con BitLocker (TPM y contraseña de recuperación). Opcionalmente custodia la clave de recuperación en el servidor.",
		Fields:      []Field{{Name: "escrow_key", Label: "Custodiar la clave de recuperación en el servidor", Type: TypeBool, Default: true}}},
	{Key: "windows.firewall", Platform: "windows", Title: "Cortafuegos activado",
		Description: "El Firewall de Windows activo en los perfiles de dominio, privado y público."},
	{Key: "windows.defender_realtime", Platform: "windows", Title: "Protección en tiempo real de Defender",
		Description: "La protección en tiempo real de Microsoft Defender activada."},
	{Key: "windows.screen_lock", Platform: "windows", Title: "Bloqueo de pantalla por inactividad",
		Description: "El equipo se bloquea tras unos minutos sin actividad.",
		Fields:      []Field{{Name: "minutes", Label: "Minutos", Type: TypeInt, Required: true, Min: 1, Max: 60}}},
	{Key: "windows.password_policy", Platform: "windows", Title: "Contraseña de las cuentas locales",
		Description: "Longitud mínima y caducidad de las contraseñas de las cuentas locales.",
		Fields: []Field{
			{Name: "min_length", Label: "Longitud mínima", Type: TypeInt, Required: true, Min: 8, Max: 20},
			{Name: "max_age_days", Label: "Caducidad en días (0 = no caduca)", Type: TypeInt, Default: 0, Min: 0, Max: 365},
		}},
	{Key: "windows.usb_storage", Platform: "windows", Title: "Almacenamiento USB",
		Description: "Permite o bloquea las memorias y discos USB.",
		Fields:      []Field{{Name: "blocked", Label: "Bloquear", Type: TypeBool, Default: true}}},
	{Key: "windows.smb1", Platform: "windows", Title: "SMBv1 desactivado",
		Description: "Desactiva el protocolo SMB versión 1, obsoleto e inseguro."},
	{Key: "windows.guest_account", Platform: "windows", Title: "Cuenta de invitado desactivada",
		Description: "La cuenta local Invitado desactivada."},
	{Key: "windows.autologon", Platform: "windows", Title: "Sin inicio de sesión automático",
		Description: "Elimina el inicio de sesión automático configurado en el registro."},
	{Key: "windows.remote_desktop", Platform: "windows", Title: "Escritorio remoto",
		Description: "Permite o deniega las conexiones de Escritorio remoto al equipo.",
		Fields:      []Field{{Name: "allowed", Label: "Permitir", Type: TypeBool, Default: false}}},
	withCheck(Def{Key: "windows.domain_join", Platform: "windows", Title: "Unir al dominio",
		Description: "Une el equipo al dominio de Active Directory. Si no encuentra un controlador (sin VPN ni red de la oficina), lo reintenta en cada evaluación. No saca al equipo de otro dominio.",
		Fields: []Field{
			{Name: "domain", Label: "Dominio", Type: TypeString, Required: true, MaxLen: 255, Description: "Nombre DNS completo, por ejemplo empresa.local."},
			{Name: "ou", Label: "Unidad organizativa (opcional)", Type: TypeString, MaxLen: 512, Description: "Por ejemplo OU=Equipos,DC=empresa,DC=local. Vacío: la predeterminada."},
			{Name: "username", Label: "Cuenta con permiso para unir equipos", Type: TypeString, Required: true, MaxLen: 256, Description: "EMPRESA\\usuario o usuario@empresa.local."},
			{Name: "password", Label: "Contraseña de esa cuenta", Type: TypeSecret, Required: true, MaxLen: 256},
			{Name: "restart", Label: "Reinicio tras unirse", Type: TypeEnum, Default: "warn_5min", Options: []string{"warn_5min", "manual"},
				OptionLabels: map[string]string{"warn_5min": "Reiniciar con aviso de 5 minutos", "manual": "No reiniciar (lo hará el usuario)"}},
		}}, checkDomainJoin),
	{Key: "windows.power", Platform: "windows", Title: "Energía",
		Description: "Plan de energía, apagado de pantalla, suspensión e hibernación. Lo que dejes vacío no se toca.",
		Fields: []Field{
			{Name: "plan", Label: "Plan de energía", Type: TypeEnum, Options: []string{"balanced", "high_performance", "power_saver"},
				OptionLabels: map[string]string{"balanced": "Equilibrado", "high_performance": "Alto rendimiento", "power_saver": "Economizador"}},
			{Name: "monitor_ac", Label: "Apagar la pantalla, enchufado (minutos, 0 = nunca)", Type: TypeInt, Min: 0, Max: 600},
			{Name: "monitor_dc", Label: "Apagar la pantalla, con batería (minutos, 0 = nunca)", Type: TypeInt, Min: 0, Max: 600},
			{Name: "sleep_ac", Label: "Suspender, enchufado (minutos, 0 = nunca)", Type: TypeInt, Min: 0, Max: 600},
			{Name: "sleep_dc", Label: "Suspender, con batería (minutos, 0 = nunca)", Type: TypeInt, Min: 0, Max: 600},
			{Name: "hibernate", Label: "Hibernación", Type: TypeEnum, Options: []string{"off", "on"},
				OptionLabels: map[string]string{"off": "Desactivada", "on": "Activada"}},
		}},
	customScript("windows", []string{"powershell"}),

	{Key: "linux.firewall", Platform: "linux", Title: "Cortafuegos activado",
		Description: "ufw o firewalld (el que tenga el equipo) activo. Si no tiene ninguno, el ajuste no aplica."},
	{Key: "linux.auto_security_updates", Platform: "linux", Title: "Actualizaciones de seguridad automáticas",
		Description: "unattended-upgrades (Debian y Ubuntu) o dnf-automatic (RHEL y derivados) instalando solo actualizaciones de seguridad."},
	{Key: "linux.ssh", Platform: "linux", Title: "Configuración de SSH",
		Description: "Acceso de root y autenticación por contraseña en el servidor SSH. La configuración se valida antes de aplicarse.",
		Fields: []Field{
			{Name: "root_login", Label: "Permitir el acceso de root", Type: TypeBool, Default: false},
			{Name: "password_auth", Label: "Permitir la autenticación por contraseña", Type: TypeBool, Default: false},
		}},
	{Key: "linux.disk_encryption", Platform: "linux", Title: "Cifrado de disco", AuditOnly: true,
		Description: "Comprueba que los volúmenes estén cifrados con LUKS. Solo se puede auditar: cifrar un disco en uso no se hace a distancia."},
	{Key: "linux.forbidden_packages", Platform: "linux", Title: "Paquetes prohibidos", AuditOnly: true,
		Description: "Avisa si alguno de los paquetes de la lista está instalado.",
		Fields:      []Field{{Name: "names", Label: "Paquetes", Type: TypeStringList, Required: true, Min: 1, Max: 50}}},
	customScript("linux", []string{"sh", "bash", "python3"}),

	android("android.password", "Contraseña de desbloqueo", "Exige un código de desbloqueo con la complejidad y longitud indicadas.",
		Field{Name: "quality", Label: "Tipo", Type: TypeEnum, Required: true, Options: []string{"numeric", "alphanumeric", "complex"}},
		Field{Name: "min_length", Label: "Longitud mínima", Type: TypeInt, Required: true, Min: 4, Max: 16}),
	android("android.screen_lock", "Bloqueo automático", "La pantalla se bloquea tras unos minutos sin uso.",
		Field{Name: "minutes", Label: "Minutos", Type: TypeInt, Required: true, Min: 1, Max: 60}),
	android("android.camera", "Cámara", "Permite o desactiva la cámara.", Field{Name: "disabled", Label: "Desactivar", Type: TypeBool, Default: true}),
	android("android.screen_capture", "Capturas de pantalla", "Permite o impide las capturas de pantalla.",
		Field{Name: "disabled", Label: "Desactivar", Type: TypeBool, Default: true}),
	android("android.usb_file_transfer", "Transferencia de ficheros por USB", "Permite o impide copiar ficheros por USB.",
		Field{Name: "disabled", Label: "Desactivar", Type: TypeBool, Default: true}),
	android("android.factory_reset", "Restablecimiento de fábrica", "Impide que el usuario restablezca el móvil desde los ajustes.",
		Field{Name: "disabled", Label: "Desactivar", Type: TypeBool, Default: true}),
	android("android.developer_options", "Opciones de desarrollador", "Permite o bloquea las opciones de desarrollador.",
		Field{Name: "disabled", Label: "Bloquear", Type: TypeBool, Default: true}),
	android("android.unknown_sources", "Aplicaciones de origen desconocido", "Permite o impide instalar aplicaciones fuera de Google Play.",
		Field{Name: "disabled", Label: "Impedir", Type: TypeBool, Default: true}),
	android("android.wifi", "Wi-Fi de la empresa", "Configura una red Wi-Fi con contraseña (WPA-PSK) que el móvil usará sola.",
		Field{Name: "ssid", Label: "Nombre de la red (SSID)", Type: TypeString, Required: true, MaxLen: 32},
		Field{Name: "passphrase", Label: "Contraseña", Type: TypeSecret, Required: true, MaxLen: 63}),
	withCheck(android("android.system_updates", "Actualizaciones del sistema", "Cuándo instala el móvil las actualizaciones de Android.",
		Field{Name: "type", Label: "Modo", Type: TypeEnum, Required: true, Options: []string{"automatic", "windowed", "postpone"}},
		Field{Name: "window_start", Label: "Desde", Type: TypeTime},
		Field{Name: "window_end", Label: "Hasta", Type: TypeTime}),
		func(p map[string]any) error {
			if p["type"] == "windowed" && (p["window_start"] == nil || p["window_end"] == nil) {
				return fmt.Errorf("Con el modo «windowed» hay que indicar el inicio y el fin de la ventana.")
			}
			return nil
		}),
	android("android.kiosk", "Modo quiosco", "El móvil solo muestra una aplicación (lectores de almacén, tabletas de recepción).",
		Field{Name: "package", Label: "Aplicación (nombre de paquete)", Type: TypePackage, Required: true}),
}

func android(key, title, desc string, fields ...Field) Def {
	return Def{Key: key, Platform: "android", Title: title, Description: desc, EnforceOnly: true, Fields: fields}
}

func checkDomainJoin(p map[string]any) error {
	if d, _ := p["domain"].(string); !domainRe.MatchString(d) {
		return fmt.Errorf("El dominio debe ser un nombre DNS completo, por ejemplo empresa.local.")
	}
	if ou, _ := p["ou"].(string); ou != "" && !ouRe.MatchString(ou) {
		return fmt.Errorf("La unidad organizativa debe tener la forma OU=Equipos,DC=empresa,DC=local.")
	}
	return nil
}

func withCheck(d Def, f func(map[string]any) error) Def {
	d.check = f
	return d
}

var (
	domainRe  = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,62}\.)+[a-z0-9-]{2,63}$`)
	ouRe      = regexp.MustCompile(`^(?i)(OU|CN)=[^=]+(,\s*(OU|CN|DC)=[^=,]+)*$`)
	hhmmRe    = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	packageRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)
)

// PackageNameValid informa de si s es un nombre de paquete de Android.
func PackageNameValid(s string) bool { return packageRe.MatchString(s) }

// Setting es un ajuste tal como lo guarda una política.
type Setting struct {
	Key    string          `json:"key"`
	Mode   string          `json:"mode"`
	Params json.RawMessage `json:"params"`
}

// Find devuelve la definición de un ajuste para una plataforma.
func Find(platform, key string) (Def, bool) {
	for _, d := range Catalog {
		if d.Platform == platform && d.Key == key {
			return d, true
		}
	}
	return Def{}, false
}

// ForPlatform devuelve los ajustes de una plataforma.
func ForPlatform(platform string) []Def {
	out := []Def{}
	for _, d := range Catalog {
		if d.Platform == platform {
			if d.Fields == nil {
				d.Fields = []Field{} // la consola los recorre: nunca null
			}
			out = append(out, d)
		}
	}
	return out
}

// ValidateSetting comprueba clave, modo y parámetros.
func ValidateSetting(platform string, s Setting) error {
	_, err := Normalize(platform, s)
	return err
}

// Normalize valida el ajuste y devuelve sus parámetros con los valores por
// defecto aplicados y en forma canónica.
func Normalize(platform string, s Setting) (Setting, error) {
	d, ok := Find(platform, s.Key)
	if !ok {
		return s, fmt.Errorf("El ajuste «%s» no existe para esta plataforma.", s.Key)
	}
	switch s.Mode {
	case protocol.ModeAudit:
		if d.EnforceOnly {
			return s, fmt.Errorf("«%s» no se puede solo auditar: en Android se aplica siempre.", d.Title)
		}
	case protocol.ModeEnforce:
		if d.AuditOnly {
			return s, fmt.Errorf("«%s» solo se puede auditar.", d.Title)
		}
	default:
		return s, fmt.Errorf("El modo de «%s» debe ser «audit» o «enforce».", d.Title)
	}
	params := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(s.Params)) > 0 && string(bytes.TrimSpace(s.Params)) != "null" {
		if err := json.Unmarshal(s.Params, &params); err != nil {
			return s, fmt.Errorf("Los parámetros de «%s» no tienen el formato esperado.", d.Title)
		}
	}
	known := map[string]bool{}
	out := map[string]any{}
	for _, f := range d.Fields {
		known[f.Name] = true
		raw, present := params[f.Name]
		if !present || string(raw) == "null" {
			if f.Required {
				return s, fmt.Errorf("Falta «%s» en «%s».", f.Label, d.Title)
			}
			if f.Default != nil {
				out[f.Name] = f.Default
			}
			continue
		}
		v, err := checkField(f, raw)
		if err != nil {
			return s, fmt.Errorf("«%s» en «%s»: %s", f.Label, d.Title, err.Error())
		}
		if v != nil {
			out[f.Name] = v
		}
	}
	for name := range params {
		if !known[name] {
			return s, fmt.Errorf("«%s» no admite el parámetro «%s».", d.Title, name)
		}
	}
	if d.check != nil {
		if err := d.check(out); err != nil {
			return s, err
		}
	}
	b, _ := json.Marshal(out)
	s.Params = b
	return s, nil
}

func checkField(f Field, raw json.RawMessage) (any, error) {
	switch f.Type {
	case TypeInt:
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, fmt.Errorf("debe ser un número entero.")
		}
		if n < f.Min || n > f.Max {
			return nil, fmt.Errorf("debe estar entre %d y %d.", f.Min, f.Max)
		}
		return n, nil
	case TypeBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("debe ser verdadero o falso.")
		}
		return b, nil
	case TypeString, TypeText:
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, fmt.Errorf("debe ser un texto.")
		}
		if f.MaxLen > 0 && len(str) > f.MaxLen {
			return nil, fmt.Errorf("supera el tamaño máximo.")
		}
		if str == "" {
			if f.Required {
				return nil, fmt.Errorf("no puede estar vacío.")
			}
			return nil, nil
		}
		if !utf8.ValidString(str) || bytes.IndexByte([]byte(str), 0) >= 0 {
			return nil, fmt.Errorf("contiene caracteres no válidos.")
		}
		return str, nil
	case TypeTime, TypePackage:
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, fmt.Errorf("debe ser un texto.")
		}
		if f.Type == TypeTime && !hhmmRe.MatchString(str) {
			return nil, fmt.Errorf("debe ser una hora con el formato HH:MM.")
		}
		if f.Type == TypePackage && !packageRe.MatchString(str) {
			return nil, fmt.Errorf("debe ser un nombre de paquete de Android, por ejemplo com.empresa.app.")
		}
		return str, nil
	case TypeSecret:
		var str string
		if err := json.Unmarshal(raw, &str); err != nil || str == "" {
			return nil, fmt.Errorf("no puede estar vacío.")
		}
		if f.MaxLen > 0 && len(str) > f.MaxLen && !strings.HasPrefix(str, "v1:") {
			return nil, fmt.Errorf("supera el tamaño máximo.")
		}
		return str, nil
	case TypeEnum:
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, fmt.Errorf("debe ser un texto.")
		}
		for _, o := range f.Options {
			if o == str {
				return str, nil
			}
		}
		return nil, fmt.Errorf("«%s» no es una opción válida.", str)
	case TypeStringList:
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("debe ser una lista de textos.")
		}
		clean := []string{}
		for _, x := range list {
			if x != "" {
				clean = append(clean, x)
			}
		}
		if len(clean) < f.Min || len(clean) > f.Max {
			return nil, fmt.Errorf("debe tener entre %d y %d elementos.", f.Min, f.Max)
		}
		return clean, nil
	}
	return nil, fmt.Errorf("tipo de parámetro desconocido.")
}
