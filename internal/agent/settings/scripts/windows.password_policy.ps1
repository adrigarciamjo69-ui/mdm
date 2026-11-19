    $minLength = [int](Get-Param 'MIN_LENGTH' 12)
    $maxAge = [int](Get-Param 'MAX_AGE_DAYS' 0)
    if ($Mode -eq 'Set') {
        $age = if ($maxAge -eq 0) { 'UNLIMITED' } else { "$maxAge" }
        $out = & net.exe accounts "/minpwlen:$minLength" "/maxpwage:$age" 2>&1
        if ($LASTEXITCODE -ne 0) { throw ("net accounts: " + ($out -join ' ')) }
        Write-Done
        return
    }
    # secedit exporta la directiva con claves que no dependen del idioma de Windows.
    $file = Join-Path $env:TEMP ("mdm-secpol-" + [guid]::NewGuid().ToString('N') + '.inf')
    try {
        & secedit.exe /export /cfg $file /areas SECURITYPOLICY /quiet | Out-Null
        $content = Get-Content -Path $file -ErrorAction Stop
    }
    finally {
        Remove-Item -Path $file -Force -ErrorAction SilentlyContinue
    }
    $len = [int](($content | Where-Object { $_ -match '^\s*MinimumPasswordLength\s*=' }) -replace '.*=\s*', '')
    $age = [int](($content | Where-Object { $_ -match '^\s*MaximumPasswordAge\s*=' }) -replace '.*=\s*', '')
    $problems = @()
    if ($len -lt $minLength) { $problems += "longitud mínima $len (se exige $minLength)" }
    if ($maxAge -eq 0 -and $age -ne -1) { $problems += "las contraseñas caducan a los $age días (se exige que no caduquen)" }
    if ($maxAge -gt 0 -and ($age -eq -1 -or $age -gt $maxAge)) { $problems += "caducidad de $age días (máximo $maxAge)" }
    if ($problems.Count -eq 0) {
        Write-Result $true "Longitud mínima $len; caducidad $age días."
    }
    else {
        Write-Result $false (($problems -join '; ') + '.')
    }
