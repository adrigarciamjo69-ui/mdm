    $path = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'
    if ($Mode -eq 'Set') {
        Set-ItemProperty -Path $path -Name 'AutoAdminLogon' -Value '0' -Type String
        Remove-ItemProperty -Path $path -Name 'DefaultPassword' -ErrorAction SilentlyContinue
        Write-Done
        return
    }
    $auto = Get-RegistryValue $path 'AutoAdminLogon'
    $password = Get-RegistryValue $path 'DefaultPassword'
    if ($auto -eq '1') {
        Write-Result $false 'El inicio de sesión automático está activado.'
    }
    elseif ($null -ne $password) {
        Write-Result $false 'Hay una contraseña guardada en el registro para el inicio automático.'
    }
    else {
        Write-Result $true 'Sin inicio de sesión automático.'
    }
