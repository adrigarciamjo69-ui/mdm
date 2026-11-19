    $path = 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server'
    $allowed = Test-Flag 'ALLOWED' $false
    # Grupo de reglas de Escritorio remoto por su recurso, que no depende del idioma.
    $group = '@FirewallAPI.dll,-28752'
    if ($Mode -eq 'Set') {
        Set-RegistryDword $path 'fDenyTSConnections' $(if ($allowed) { 0 } else { 1 })
        if ($allowed) {
            Enable-NetFirewallRule -Group $group -ErrorAction SilentlyContinue
        }
        else {
            Disable-NetFirewallRule -Group $group -ErrorAction SilentlyContinue
        }
        Write-Done
        return
    }
    $deny = Get-RegistryValue $path 'fDenyTSConnections'
    $isAllowed = $deny -eq 0
    $state = if ($isAllowed) { 'permitido' } else { 'denegado' }
    Write-Result ($isAllowed -eq $allowed) "Escritorio remoto $state."
