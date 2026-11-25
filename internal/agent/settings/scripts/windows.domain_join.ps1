    # Une el equipo a un dominio de Active Directory. La contraseña llega en
    # MDM_PARAM_PASSWORD (el servidor la descifra solo para este equipo) y no
    # se escribe nunca en la salida.
    $domain = Get-Param 'DOMAIN'
    $ou = Get-Param 'OU'
    $restart = Get-Param 'RESTART' 'warn_5min'
    $markPath = 'HKLM:\SOFTWARE\MDM\DomainJoin'
    $cs = Get-CimInstance Win32_ComputerSystem
    $inDomain = $cs.PartOfDomain -and ($cs.Domain -ieq $domain)
    $pending = (Get-RegistryValue $markPath 'Pending') -ieq $domain
    if ($Mode -eq 'Set') {
        if ($inDomain) { Write-Done "Ya está en el dominio $domain."; return }
        if ($pending) { Write-Done "Unido a $domain; pendiente de reinicio."; return }
        if ($cs.PartOfDomain) {
            throw "El equipo está en el dominio $($cs.Domain); sácalo a mano antes de unirlo a $domain."
        }
        & nltest.exe "/dsgetdc:$domain" | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "No se encuentra ningún controlador del dominio $domain (¿sin VPN o fuera de la oficina?). Se reintentará en la próxima evaluación."
        }
        $secure = ConvertTo-SecureString (Get-Param 'PASSWORD') -AsPlainText -Force
        $cred = New-Object System.Management.Automation.PSCredential((Get-Param 'USERNAME'), $secure)
        $joinArgs = @{ DomainName = $domain; Credential = $cred; Force = $true }
        if ($ou) { $joinArgs.OUPath = $ou }
        Add-Computer @joinArgs -WarningAction SilentlyContinue
        if (-not (Test-Path $markPath)) { New-Item -Path $markPath -Force | Out-Null }
        New-ItemProperty -Path $markPath -Name 'Pending' -Value $domain -PropertyType String -Force | Out-Null
        if ($restart -eq 'warn_5min') {
            & shutdown.exe /r /t 300 /d p:4:2 /c "El equipo se ha unido al dominio $domain y se reiniciará en 5 minutos. Guarda tu trabajo." | Out-Null
            Write-Done "Unido al dominio $domain; se reinicia en 5 minutos."
        }
        else {
            Write-Done "Unido al dominio $domain; falta reiniciar."
        }
        return
    }
    if ($inDomain) {
        if ($pending) { Remove-ItemProperty -Path $markPath -Name 'Pending' -ErrorAction SilentlyContinue }
        Write-Result $true "En el dominio $domain."
    }
    elseif ($pending) {
        Write-Result $true "Unido a $domain; pendiente de reinicio."
    }
    elseif ($cs.PartOfDomain) {
        Write-Result $false "Está en otro dominio: $($cs.Domain)."
    }
    else {
        Write-Result $false "No está en el dominio $domain."
    }
