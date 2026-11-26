//go:build windows

// Package winsession averigua qué usuario tiene la sesión abierta en Windows.
// Win32_ComputerSystem.UserName solo informa de la consola física y se queda
// vacío con Escritorio remoto o con el cambio rápido de usuario; aquí se
// recorren las sesiones de Terminal Services y se lee el usuario de cada una.
package winsession

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ActiveUsers devuelve DOMINIO\usuario de las sesiones activas, primero la
// de la consola. Necesita ejecutarse como SYSTEM (el servicio del agente).
func ActiveUsers() []string {
	var info *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &info, &count); err != nil {
		return nil
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(info)))
	console := windows.WTSGetActiveConsoleSessionId()
	var first, rest []string
	for _, s := range unsafe.Slice(info, count) {
		if s.State != windows.WTSActive {
			continue
		}
		u := sessionUser(s.SessionID)
		if u == "" {
			continue
		}
		if s.SessionID == console {
			first = append(first, u)
		} else {
			rest = append(rest, u)
		}
	}
	return append(first, rest...)
}

// ActiveUser devuelve el primer usuario con sesión activa, o "".
func ActiveUser() string {
	if u := ActiveUsers(); len(u) > 0 {
		return u[0]
	}
	return ""
}

func sessionUser(id uint32) string {
	var tok windows.Token
	if err := windows.WTSQueryUserToken(id, &tok); err != nil {
		return ""
	}
	defer tok.Close()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return ""
	}
	account, domain, _, err := tu.User.Sid.LookupAccount("")
	if err != nil || account == "" {
		return ""
	}
	if domain == "" {
		return account
	}
	return domain + `\` + account
}
